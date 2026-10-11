package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestCaptchaTokensRequireHS256ExpiryAndMatchingIP(t *testing.T) {
	s, _ := newJWTTestService(t)
	for _, tc := range []struct {
		name    string
		method  jwt.SigningMethod
		expires *jwt.NumericDate
		ip      string
	}{
		{"valid", jwt.SigningMethodHS256, jwt.NewNumericDate(time.Now().Add(time.Minute)), "192.0.2.1"},
		{"HS384", jwt.SigningMethodHS384, jwt.NewNumericDate(time.Now().Add(time.Minute)), "192.0.2.1"},
		{"HS512", jwt.SigningMethodHS512, jwt.NewNumericDate(time.Now().Add(time.Minute)), "192.0.2.1"},
		{"missing expiry", jwt.SigningMethodHS256, nil, "192.0.2.1"},
		{"expired", jwt.SigningMethodHS256, jwt.NewNumericDate(time.Now().Add(-time.Minute)), "192.0.2.1"},
		{"other IP", jwt.SigningMethodHS256, jwt.NewNumericDate(time.Now().Add(time.Minute)), "192.0.2.2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token, err := jwt.NewWithClaims(tc.method, CaptchaClaims{IP: tc.ip, RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: tc.expires}}).SignedString([]byte(s.Config().JwtSecretKey + "_captcha"))
			if err != nil {
				t.Fatal(err)
			}
			if got := s.VerifyCaptchaJWT(token, "192.0.2.1"); got != (tc.name == "valid") {
				t.Fatalf("captcha verification = %v", got)
			}
		})
	}
}
