package controllers

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/middlewares"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/services/tusupload"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/patrickmn/go-cache"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Exercise the public callers with legacy credentials, including the separate
// upload server, so a verifier left behind cannot keep a revoked session alive.
func TestSessionRevocationBoundaries(t *testing.T) {
	for _, change := range []string{"demotion", "deletion", "password reset", "self password change"} {
		t.Run(change, func(t *testing.T) {
			h, user := newSessionHandlers(t)
			token := legacySessionToken(t, h, user)
			assertSessionBoundaries(t, h, token, false)

			switch change {
			case "demotion":
				admin := false
				if status, _, err := h.Logic.UpdateUser(uint64(user.ID), "", "", &admin, nil, nil, nil, nil, nil); err != nil || status != http.StatusOK {
					t.Fatalf("demote: %d %v", status, err)
				}
			case "deletion":
				if status, err := h.Logic.DeleteUser(uint64(user.ID)); err != nil || status != http.StatusNoContent {
					t.Fatalf("delete: %d %v", status, err)
				}
			case "password reset":
				sessionUserMutation(t, h.ResetUserPassword, user.ID, `{"new_password":"replacement-password"}`)
			case "self password change":
				req := httptest.NewRequest(http.MethodPost, "/api/user/settings", strings.NewReader(`{"NewPassword":"replacement-password"}`))
				req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
				rec := httptest.NewRecorder()
				c := echo.New().NewContext(req, rec)
				c.Set("UserID", user.ID)
				if err := h.UpdateUserSettingsController(c); err != nil || rec.Code != http.StatusOK {
					t.Fatalf("self password change: %d %v", rec.Code, err)
				}
			}
			assertSessionBoundaries(t, h, token, true)
			if change != "deletion" {
				password := "original-password"
				if strings.Contains(change, "password") {
					password = "replacement-password"
				}
				login := sessionLogin(t, h, user.Username, password)
				assertSessionBoundariesWithoutRefresh(t, h, login, false)
				if _, _, err := h.Auth.RefreshJWT(login); err == nil {
					t.Fatal("new login bypassed the five-minute refresh window")
				}
			}
		})
	}
}

func TestDeletedAPIKeyOwnerRejectedAtEveryBoundary(t *testing.T) {
	h, user := newSessionHandlers(t)
	key := models.ApiKey{UserID: user.ID, Key: "ak_live-owner", Name: "test"}
	if err := h.Deps.DB.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	assertSessionBoundariesWithoutRefresh(t, h, key.Key, false)
	if status, err := h.Logic.ResetUserPassword(uint64(user.ID), "replacement-password"); err != nil || status != http.StatusOK {
		t.Fatalf("reset: %d %v", status, err)
	}
	// API keys have their own lifetime and survive JWT password revocation.
	assertSessionBoundariesWithoutRefresh(t, h, key.Key, false)
	if err := h.Deps.DB.Delete(&user).Error; err != nil {
		t.Fatal(err)
	}
	assertSessionBoundariesWithoutRefresh(t, h, key.Key, true)
	if err := h.Deps.DB.Unscoped().Delete(&user).Error; err != nil {
		t.Fatal(err)
	}
	assertSessionBoundariesWithoutRefresh(t, h, key.Key, true)
}

func TestServerLogoutRevokesEveryJWTConsumerAndKeepsAPIKeys(t *testing.T) {
	h, user := newSessionHandlers(t)
	token := legacySessionToken(t, h, user)
	key := models.ApiKey{UserID: user.ID, Key: "ak_session-logout-control", Name: "test"}
	if err := h.Deps.DB.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		if err := h.AuthLogout(echo.New().NewContext(req, rec)); err != nil || rec.Code != http.StatusNoContent {
			t.Fatalf("logout attempt %d: %d %v", attempt, rec.Code, err)
		}
	}
	assertSessionBoundaries(t, h, token, true)
	assertSessionBoundariesWithoutRefresh(t, h, key.Key, false)
}

func TestConcurrentSessionRevocationsDoNotLoseVersion(t *testing.T) {
	h, user := newSessionHandlers(t)
	token := legacySessionToken(t, h, user)
	start := make(chan struct{})
	results := make(chan error, 3)
	var wg sync.WaitGroup
	for _, password := range []string{"first-password", "second-password"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := h.Logic.ResetUserPassword(uint64(user.ID), password)
			results <- err
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		admin := false
		_, _, err := h.Logic.UpdateUser(uint64(user.ID), "", "", &admin, nil, nil, nil, nil, nil)
		results <- err
	}()
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var current models.User
	if err := h.Deps.DB.First(&current, user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.SessionVersion != 3 || current.Admin {
		t.Fatalf("concurrent revocations lost state: version=%d admin=%v", current.SessionVersion, current.Admin)
	}
	assertSessionBoundaries(t, h, token, true)
}

func TestStaleUserWritesPreserveConcurrentRevocation(t *testing.T) {
	for _, writer := range []string{"profile update", "same role update", "password reset", "self password change"} {
		t.Run(writer, func(t *testing.T) {
			h, user := newSessionHandlers(t)
			injected := false
			// Commit the concurrent change after the writer reads its old user
			// snapshot. This gives a deterministic stale-read/write interleaving.
			if err := h.Deps.DB.Callback().Query().After("gorm:query").Register("test:concurrent_revocation", func(tx *gorm.DB) {
				if injected || tx.Statement.Table != "users" {
					return
				}
				injected = true
				if err := h.Deps.DB.Exec("UPDATE users SET admin = 0, hash = ?, session_version = session_version + 1 WHERE id = ?", "concurrent-password-hash", user.ID).Error; err != nil {
					tx.AddError(err)
				}
			}); err != nil {
				t.Fatal(err)
			}
			wantVersion := uint64(1)
			switch writer {
			case "profile update", "same role update":
				var admin *bool
				if writer == "same role update" {
					value := false
					admin = &value
				}
				if status, _, err := h.Logic.UpdateUser(uint64(user.ID), "renamed", "", admin, nil, nil, nil, nil, nil); err != nil || status != http.StatusOK {
					t.Fatalf("update: %d %v", status, err)
				}
			case "password reset":
				wantVersion = 2
				sessionUserMutation(t, h.ResetUserPassword, user.ID, `{"new_password":"replacement-password"}`)
			case "self password change":
				wantVersion = 2
				req := httptest.NewRequest(http.MethodPost, "/api/user/settings", strings.NewReader(`{"NewPassword":"replacement-password"}`))
				req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
				rec := httptest.NewRecorder()
				c := echo.New().NewContext(req, rec)
				c.Set("UserID", user.ID)
				if err := h.UpdateUserSettingsController(c); err != nil || rec.Code != http.StatusOK {
					t.Fatalf("self password change: %d %v", rec.Code, err)
				}
			}
			var current models.User
			if err := h.Deps.DB.First(&current, user.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !injected || current.Admin || current.SessionVersion != wantVersion {
				t.Fatalf("stale write restored role/version: admin=%v version=%d", current.Admin, current.SessionVersion)
			}
			if wantVersion == 1 && current.Hash != "concurrent-password-hash" {
				t.Fatal("profile/role update restored stale password hash")
			}
			if wantVersion == 2 && bcrypt.CompareHashAndPassword([]byte(current.Hash), []byte("replacement-password")) != nil {
				t.Fatal("password writer did not retain new password")
			}
		})
	}
}

func TestUnchangedRoleAndProfileSettingsKeepLegacySession(t *testing.T) {
	h, user := newSessionHandlers(t)
	token := legacySessionToken(t, h, user)
	admin, enabled := true, false
	storage, balance, maxDownloads, pool := int64(123), float64(0), 7, uint(0)
	status, response, err := h.Logic.UpdateUser(uint64(user.ID), "renamed", "new@example.com", &admin, &storage, &balance, &maxDownloads, &enabled, &pool)
	if err != nil || status != http.StatusOK {
		t.Fatalf("profile update: %d %v", status, err)
	}
	if response.Username != "renamed" || response.Email != "new@example.com" || response.Storage != 123 || response.Balance != 0 || response.MaxRemoteDownloads != 7 || response.RemoteDownloadEnabled || response.StoragePoolID != nil {
		t.Fatalf("profile settings were not saved: %+v", response)
	}
	assertSessionBoundaries(t, h, token, false)
	_, claims, err := h.Auth.VerifyJWT(token)
	if err != nil || claims.Username != "renamed" || !claims.Admin || claims.SessionVersion != 0 {
		t.Fatalf("unchanged role session: %+v %v", claims, err)
	}
}

func sessionUserMutation(t *testing.T, handler echo.HandlerFunc, userID uint, body string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/users/1/password", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(strconv.FormatUint(uint64(userID), 10))
	if err := handler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("user mutation: %d %v: %s", rec.Code, err, rec.Body.String())
	}
}

func newSessionHandlers(t *testing.T) (*Handlers, models.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "sessions.db")), &gorm.Config{
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
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.User{}, &models.RevokedAuthSession{}, &models.ApiKey{}, &models.ApiKeyAuditLog{}, &models.UploadSession{}); err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("original-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user := models.User{Username: "tester", Admin: true, Hash: string(hash)}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	captcha := false
	deps := &app.Deps{DB: db, Cache: cache.New(time.Minute, time.Minute), Snapshots: app.NewSnapshotStore(app.Snapshot{Config: config.Config{
		JwtSecretKey: "session-secret", CaptchaLoginEnabled: &captcha, FolderVideoUploadsPriv: t.TempDir(),
	}})}
	a := auth.NewService(deps)
	tus := tusupload.NewService(deps, a)
	t.Cleanup(tus.Close)
	return NewHandlers(deps, a, logic.NewService(deps), nil, tus), user
}

func legacySessionToken(t *testing.T, h *Handlers, user models.User) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"userid": user.ID, "username": user.Username, "admin": user.Admin,
		"exp": time.Now().Add(2 * time.Minute).Unix(),
	}).SignedString([]byte(h.Config().JwtSecretKey))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func assertSessionBoundaries(t *testing.T, h *Handlers, token string, revoked bool) {
	t.Helper()
	assertSessionBoundariesWithoutRefresh(t, h, token, revoked)
	rec := sessionRequest(t, h.AuthRefresh, token)
	want := http.StatusOK
	if revoked {
		want = http.StatusForbidden
	}
	if rec.Code != want {
		t.Errorf("refresh status = %d, want %d: %s", rec.Code, want, rec.Body.String())
	}
}

func assertSessionBoundariesWithoutRefresh(t *testing.T, h *Handlers, token string, revoked bool) {
	t.Helper()
	factory := middlewares.NewFactory(h.Deps, h.Auth)
	for name, handler := range map[string]echo.HandlerFunc{
		"auth/check": h.AuthCheck,
		"middleware": factory.AuthMiddleware()(func(c echo.Context) error { return c.NoContent(http.StatusOK) }),
	} {
		rec := sessionRequest(t, handler, token)
		want := http.StatusOK
		if revoked {
			want = http.StatusForbidden
		}
		if rec.Code != want {
			t.Errorf("%s status = %d, want %d", name, rec.Code, want)
		}
	}
	req := httptest.NewRequest(http.MethodHead, tusupload.BasePath+"missing-upload", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Tus-Resumable", "1.0.0")
	rec := httptest.NewRecorder()
	h.TUS.ServeHTTP(rec, req)
	want := http.StatusNotFound
	if revoked {
		want = http.StatusForbidden
	}
	if rec.Code != want {
		t.Errorf("tus status = %d, want %d: %s", rec.Code, want, rec.Body.String())
	}
}

func sessionRequest(t *testing.T, handler echo.HandlerFunc, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/check", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	if err := handler(echo.New().NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	return rec
}

func sessionLogin(t *testing.T, h *Handlers, username, password string) string {
	t.Helper()
	form := strings.NewReader("username=" + username + "&password=" + password)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", form)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	if err := h.AuthLogin(echo.New().NewContext(req, rec)); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("login: %d %v: %s", rec.Code, err, rec.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Token == "" {
		t.Fatalf("login response: %v %s", err, rec.Body.String())
	}
	return body.Token
}
