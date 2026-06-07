package auth

import (
	"ch/kirari04/videocms/config"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestGenerateAndVerifyMediaToken(t *testing.T) {
	restore := setMediaSecret("media-secret")
	defer restore()

	tokenString, expires, err := GenerateMediaToken(MediaClaims{
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

	token, claims, err := VerifyMediaToken(tokenString)
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
	restore := setMediaSecret("media-secret")
	tokenString, _, err := GenerateMediaToken(MediaClaims{LinkUUID: "link-uuid", FileUUID: "file-uuid"})
	restore()
	if err != nil {
		t.Fatalf("GenerateMediaToken() error = %v", err)
	}

	restore = setMediaSecret("different-secret")
	defer restore()

	if _, _, err := VerifyMediaToken(tokenString); err == nil {
		t.Fatal("VerifyMediaToken() accepted a token signed with a different secret")
	}
}

func TestVerifyMediaTokenRejectsExpiredToken(t *testing.T) {
	restore := setMediaSecret("media-secret")
	defer restore()

	tokenString := signMediaClaims(t, MediaClaims{
		LinkUUID: "link-uuid",
		FileUUID: "file-uuid",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "link-uuid",
			Audience:  jwt.ClaimStrings{mediaAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
		},
	})

	if _, _, err := VerifyMediaToken(tokenString); err == nil {
		t.Fatal("VerifyMediaToken() accepted an expired token")
	}
}

func TestVerifyMediaTokenRejectsWrongAudience(t *testing.T) {
	restore := setMediaSecret("media-secret")
	defer restore()

	tokenString := signMediaClaims(t, MediaClaims{
		LinkUUID: "link-uuid",
		FileUUID: "file-uuid",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "link-uuid",
			Audience:  jwt.ClaimStrings{"wrong-audience"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})

	if _, _, err := VerifyMediaToken(tokenString); err == nil {
		t.Fatal("VerifyMediaToken() accepted a token with the wrong audience")
	}
}

func TestVerifyMediaTokenRejectsSubjectMismatch(t *testing.T) {
	restore := setMediaSecret("media-secret")
	defer restore()

	tokenString := signMediaClaims(t, MediaClaims{
		LinkUUID: "link-uuid",
		FileUUID: "file-uuid",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "other-link-uuid",
			Audience:  jwt.ClaimStrings{mediaAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})

	if _, _, err := VerifyMediaToken(tokenString); err == nil {
		t.Fatal("VerifyMediaToken() accepted mismatched subject and link UUID")
	}
}

func setMediaSecret(secret string) func() {
	previous := config.ENV.JwtMediaSecretKey
	config.ENV.JwtMediaSecretKey = secret
	return func() {
		config.ENV.JwtMediaSecretKey = previous
	}
}

func signMediaClaims(t *testing.T, claims MediaClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(config.ENV.JwtMediaSecretKey))
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}
	return tokenString
}
