package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type CaptchaClaims struct {
	IP string `json:"ip"`
	jwt.RegisteredClaims
}

var captchaSessionDuration = time.Minute * 20

func (s *Service) GenerateCaptchaJWT(ip string) (string, time.Time, error) {
	expirationTime := time.Now().Add(captchaSessionDuration)
	key := []byte(s.Config().JwtSecretKey + "_captcha")

	claims := &CaptchaClaims{
		IP: ip,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(key)
	return tokenString, expirationTime, err
}

func (s *Service) VerifyCaptchaJWT(tokenString, ip string) bool {
	key := []byte(s.Config().JwtSecretKey + "_captcha")
	claims := &CaptchaClaims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return key, nil
	})

	if err != nil || !token.Valid {
		return false
	}

	if claims.IP != ip {
		return false
	}

	return true
}
