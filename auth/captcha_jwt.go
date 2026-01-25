package auth

import (
	"ch/kirari04/videocms/config"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type CaptchaClaims struct {
	IP string `json:"ip"`
	jwt.RegisteredClaims
}

var captchaSessionDuration = time.Minute * 20

func GenerateCaptchaJWT(ip string) (string, time.Time, error) {
	expirationTime := time.Now().Add(captchaSessionDuration)
	jwtKey := []byte(config.ENV.JwtSecretKey + "_captcha")

	claims := &CaptchaClaims{
		IP: ip,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtKey)
	return tokenString, expirationTime, err
}

func VerifyCaptchaJWT(tokenString, ip string) bool {
	jwtKey := []byte(config.ENV.JwtSecretKey + "_captcha")
	claims := &CaptchaClaims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return jwtKey, nil
	})

	if err != nil || !token.Valid {
		return false
	}

	if claims.IP != ip {
		return false
	}

	return true
}