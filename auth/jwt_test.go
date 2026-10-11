package auth

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/models"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestVerifyJWTUsesLiveIdentityAndRefreshClaims(t *testing.T) {
	s, user := newJWTTestService(t)
	legacy := jwt.MapClaims{"userid": user.ID, "username": "old-name", "admin": true, "exp": time.Now().Add(2 * time.Minute).Unix()}
	tokenString := signSessionClaims(t, s, jwt.SigningMethodHS256, legacy)
	_, claims, err := s.VerifyJWT(tokenString)
	if err != nil || claims.Username != user.Username || claims.Admin {
		t.Fatalf("live identity: %+v %v", claims, err)
	}
	before := time.Now()
	refreshed, expires, err := s.RefreshJWT(tokenString)
	if err != nil {
		t.Fatal(err)
	}
	if expires.Before(before.Add(7*24*time.Hour)) || expires.After(time.Now().Add(7*24*time.Hour)) {
		t.Fatalf("refresh expiration = %v, want seven days", expires)
	}
	_, claims, err = s.VerifyJWT(refreshed)
	if err != nil || claims.Username != user.Username || claims.Admin || claims.SessionVersion != 0 {
		t.Fatalf("refreshed live identity: %+v %v", claims, err)
	}
	issued := &Claims{}
	if _, err := jwt.ParseWithClaims(refreshed, issued, func(*jwt.Token) (any, error) {
		return []byte(s.Config().JwtSecretKey), nil
	}); err != nil || issued.Username != user.Username || issued.Admin {
		t.Fatalf("refresh signed stale identity: %+v %v", issued, err)
	}
	if err := s.Deps.DB.Model(&user).Update("session_version", 1).Error; err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{tokenString, refreshed} {
		if _, _, err := s.VerifyJWT(token); err == nil {
			t.Fatal("accepted old legacy/refreshed session after version change")
		}
		if _, _, err := s.RefreshJWT(token); err == nil {
			t.Fatal("refreshed revoked session")
		}
	}
	user.SessionVersion = 1
	fresh, _, err := s.GenerateJWT(user)
	if err != nil {
		t.Fatal(err)
	}
	if _, claims, err := s.VerifyJWT(fresh); err != nil || claims.SessionVersion != 1 {
		t.Fatalf("new session: %+v %v", claims, err)
	}
	if _, _, err := s.RefreshJWT(fresh); err == nil {
		t.Fatal("fresh session refreshed outside five-minute window")
	}
}

func TestVerifyJWTRejectsUnsafeClaimsAndAlgorithms(t *testing.T) {
	s, user := newJWTTestService(t)
	for _, test := range []struct {
		name   string
		method jwt.SigningMethod
		change func(jwt.MapClaims)
	}{
		{"missing expiry", jwt.SigningMethodHS256, func(c jwt.MapClaims) { delete(c, "exp") }},
		{"null expiry", jwt.SigningMethodHS256, func(c jwt.MapClaims) { c["exp"] = nil }},
		{"expired", jwt.SigningMethodHS256, func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Minute).Unix() }},
		{"different HMAC algorithm", jwt.SigningMethodHS512, func(c jwt.MapClaims) {}},
		{"no user", jwt.SigningMethodHS256, func(c jwt.MapClaims) { delete(c, "userid") }},
		{"unknown user", jwt.SigningMethodHS256, func(c jwt.MapClaims) { c["userid"] = user.ID + 100 }},
		{"revoked version", jwt.SigningMethodHS256, func(c jwt.MapClaims) { c["session_version"] = 1 }},
		{"negative version", jwt.SigningMethodHS256, func(c jwt.MapClaims) { c["session_version"] = -1 }},
		{"fractional version", jwt.SigningMethodHS256, func(c jwt.MapClaims) { c["session_version"] = 0.5 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := jwt.MapClaims{"userid": user.ID, "username": user.Username, "admin": false, "exp": time.Now().Add(time.Minute).Unix()}
			test.change(claims)
			token := signSessionClaims(t, s, test.method, claims)
			if _, _, err := s.VerifyJWT(token); err == nil {
				t.Fatal("unsafe JWT accepted")
			}
			if _, _, err := s.RefreshJWT(token); err == nil {
				t.Fatal("unsafe JWT refreshed")
			}
		})
	}
	token, _, err := s.GenerateJWT(user)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Deps.DB.Delete(&user).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.VerifyJWT(token); err == nil {
		t.Fatal("deleted account session accepted")
	}
}

func newJWTTestService(t *testing.T) (*Service, models.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth.db")), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logger.Silent),
		DisableForeignKeyConstraintWhenMigrating: true,
		IgnoreRelationshipsWhenMigrating:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.User{}, &models.RevokedAuthSession{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{Username: "current-name"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	return NewService(&app.Deps{DB: db, Snapshots: app.NewSnapshotStore(app.Snapshot{Config: config.Config{JwtSecretKey: "test-session-secret"}})}), user
}

func signSessionClaims(t *testing.T, s *Service, method jwt.SigningMethod, claims jwt.Claims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(method, claims).SignedString([]byte(s.Config().JwtSecretKey))
	if err != nil {
		t.Fatal(err)
	}
	return token
}
