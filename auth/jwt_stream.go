package auth

import (
	"ch/kirari04/videocms/config"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type ClaimsStream struct {
	UUID string `json:"uuid"`
	jwt.RegisteredClaims
}

var jwtKeyStream []byte

func GenerateJWTStream(linkUuid string) (string, time.Time, error) {
	jwtKeyStream = []byte(fmt.Sprint(config.ENV.JwtSecretKey, "-stream"))
	// Declare the expiration time of the token
	// here, we have kept it as 5 minutes
	expirationTime := time.Now().Add(24 * time.Hour)
	// Create the JWT claims, which includes the username and expiry time
	claims := &ClaimsStream{
		UUID: linkUuid,
		RegisteredClaims: jwt.RegisteredClaims{
			// In JWT, the expiry time is expressed as unix milliseconds
			ExpiresAt: jwt.NewNumericDate(expirationTime),
		},
	}

	// Declare the token with the algorithm used for signing, and the claims
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	// Create the JWT string
	tokenString, err := token.SignedString(jwtKey)
	if err != nil {
		return "", time.Now(), err
	}
	return tokenString, expirationTime, nil
}

func VerifyJWTStream(tknStr string) (*jwt.Token, *ClaimsStream, error) {
	jwtKeyStream = []byte(fmt.Sprint(config.ENV.JwtSecretKey, "-stream"))
	claims := &ClaimsStream{}

	// Parse the JWT string and store the result in `claims`.
	// Note that we are passing the key in this method as well. This method will return an error
	// if the token is invalid (if it has expired according to the expiry time we set on sign in),
	// or if the signature does not matchas expire
	tkn, err := jwt.ParseWithClaims(tknStr, claims, func(token *jwt.Token) (interface{}, error) {
		return jwtKey, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return tkn, claims, nil
}
