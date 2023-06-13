package helpers

import (
	"ch/kirari04/videocms/config"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var jwtKey []byte

func GenerateDynamicJWT[T jwt.Claims](claims *T, expire time.Duration) (string, time.Time, error) {
	jwtKey = []byte(config.ENV.JwtSecretKey)
	expirationTime := time.Now().Add(expire)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, *claims)
	tokenString, err := token.SignedString(jwtKey)
	if err != nil {
		return "", time.Now(), err
	}
	return tokenString, expirationTime, nil
}

func VerifyDynamicJWT[T jwt.Claims](tknStr string, claims *T) (*jwt.Token, error) {
	jwtKey = []byte(config.ENV.JwtSecretKey)

	tkn, err := jwt.ParseWithClaims(tknStr, *claims, func(token *jwt.Token) (interface{}, error) {
		return jwtKey, nil
	})
	if err != nil {
		return nil, err
	}
	return tkn, nil
}
