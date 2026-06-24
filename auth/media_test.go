package auth

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestGenerateAndVerifyMediaToken(t *testing.T) {
	authSvc := newTestService("media-secret")

	tokenString, expires, err := authSvc.GenerateMediaToken(MediaClaims{
		LinkUUID:      "link-uuid",
		FileUUID:      "file-uuid",
		UserID:        1,
		FileID:        2,
		QualityIDs:    map[string]uint{"720p": 3},
		AudioIDs:      map[string]uint{"audio-uuid": 4},
		SubtitleUUIDs: []string{"subtitle-uuid"},
	})
	if err != nil {
		t.Fatalf("GenerateMediaToken() error = %v", err)
	}
	if time.Until(expires) <= 0 {
		t.Fatal("GenerateMediaToken() returned an expired timestamp")
	}

	token, claims, err := authSvc.VerifyMediaToken(tokenString)
	if err != nil {
		t.Fatalf("VerifyMediaToken() error = %v", err)
	}
	if !token.Valid {
		t.Fatal("VerifyMediaToken() returned invalid token")
	}
	if claims.LinkUUID != "link-uuid" || claims.FileUUID != "file-uuid" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	if claims.QualityIDs["720p"] != 3 {
		t.Fatalf("quality id was not preserved: %+v", claims.QualityIDs)
	}
}

func TestVerifyMediaTokenRejectsWrongSecret(t *testing.T) {
	authSvc := newTestService("media-secret")
	tokenString, _, err := authSvc.GenerateMediaToken(MediaClaims{LinkUUID: "link-uuid", FileUUID: "file-uuid"})
	if err != nil {
		t.Fatalf("GenerateMediaToken() error = %v", err)
	}

	otherAuthSvc := newTestService("different-secret")

	if _, _, err := otherAuthSvc.VerifyMediaToken(tokenString); err == nil {
		t.Fatal("VerifyMediaToken() accepted a token signed with a different secret")
	}
}

func TestVerifyMediaTokenRejectsExpiredToken(t *testing.T) {
	authSvc := newTestService("media-secret")

	tokenString := signMediaClaims(t, "media-secret", MediaClaims{
		LinkUUID: "link-uuid",
		FileUUID: "file-uuid",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "link-uuid",
			Audience:  jwt.ClaimStrings{mediaAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
	})

	if _, _, err := authSvc.VerifyMediaToken(tokenString); err == nil {
		t.Fatal("VerifyMediaToken() accepted an expired token")
	}
}

func TestVerifyMediaTokenRejectsWrongAudience(t *testing.T) {
	authSvc := newTestService("media-secret")

	tokenString := signMediaClaims(t, "media-secret", MediaClaims{
		LinkUUID: "link-uuid",
		FileUUID: "file-uuid",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "link-uuid",
			Audience:  jwt.ClaimStrings{"wrong-audience"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})

	if _, _, err := authSvc.VerifyMediaToken(tokenString); err == nil {
		t.Fatal("VerifyMediaToken() accepted a token with the wrong audience")
	}
}

func TestVerifyMediaTokenRejectsSubjectMismatch(t *testing.T) {
	authSvc := newTestService("media-secret")

	tokenString := signMediaClaims(t, "media-secret", MediaClaims{
		LinkUUID: "link-uuid",
		FileUUID: "file-uuid",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "other-link-uuid",
			Audience:  jwt.ClaimStrings{mediaAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})

	if _, _, err := authSvc.VerifyMediaToken(tokenString); err == nil {
		t.Fatal("VerifyMediaToken() accepted mismatched subject and link UUID")
	}
}

func newTestService(mediaSecret string) *Service {
	return NewService(&app.Deps{
		Snapshots: app.NewSnapshotStore(app.Snapshot{
			Config: config.Config{
				JwtSecretKey:      "jwt-secret",
				JwtMediaSecretKey: mediaSecret,
			},
		}),
	})
}

func signMediaClaims(t *testing.T, secret string, claims MediaClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}
	return tokenString
}
