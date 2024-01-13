package auth

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID   uint   `json:"userid"`
	Username string `json:"username"`
	Admin    bool   `json:"admin"`
	jwt.RegisteredClaims
}

var jwtKey []byte
var sessionDuration = 15 * time.Minute

func GenerateJWT(user models.User) (string, time.Time, error) {
	jwtKey = []byte(config.ENV.JwtSecretKey)
	// Declare the expiration time of the token
	// here, we have kept it as 5 minutes
	expirationTime := time.Now().Add(sessionDuration)
	// Create the JWT claims, which includes the username and expiry time
	claims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		Admin:    user.Admin,
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

func VerifyJWT(tknStr string) (*jwt.Token, *Claims, error) {
	jwtKey = []byte(config.ENV.JwtSecretKey)
	claims := &Claims{}

	// Parse the JWT string and store the result in `claims`.
	// Note that we are passing the key in this method as well. This method will return an error
	// if the token is invalid (if it has expired according to the expiry time we set on sign in),
	// or if the signature does not match
	tkn, err := jwt.ParseWithClaims(tknStr, claims, func(token *jwt.Token) (interface{}, error) {
		return jwtKey, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return tkn, claims, nil
}

func RefreshJWT(tknStr string) (string, time.Time, error) {
	jwtKey = []byte(config.ENV.JwtSecretKey)
	claims := &Claims{}
	tkn, err := jwt.ParseWithClaims(tknStr, claims, func(token *jwt.Token) (interface{}, error) {
		return jwtKey, nil
	})
	if err != nil {
		return "", time.Now(), err
	}
	if !tkn.Valid {
		return "", time.Now(), errors.New("Invalid jwt key")
	}

	// We ensure that a new token is not issued until enough time has elapsed
	// In this case, a new token will only be issued if the old token is within
	// 30 seconds of expiry. Otherwise, return a bad request status
	if time.Until(claims.ExpiresAt.Time) > 5*time.Minute {
		return "", time.Now(), errors.New(fmt.Sprintf("Wait until time to expire: %v", claims.ExpiresAt.Time.String()))
	}

	// Now, create a n ew token for the current use, with a renewed expiration time
	expirationTime := time.Now().Add(sessionDuration)
	claims.ExpiresAt = jwt.NewNumericDate(expirationTime)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtKey)
	if err != nil {
		return "", time.Now(), err
	}

	return tokenString, expirationTime, nil
}
