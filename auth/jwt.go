package auth

import (
	"ch/kirari04/videocms/models"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

type Claims struct {
	UserID         uint   `json:"userid"`
	Username       string `json:"username"`
	Admin          bool   `json:"admin"`
	SessionVersion uint64 `json:"session_version"`
	jwt.RegisteredClaims
}

var sessionDuration = time.Hour * 24 * 7

func (s *Service) GenerateJWT(user models.User) (string, time.Time, error) {
	expirationTime := time.Now().Add(sessionDuration)
	return s.GenerateTimeJWT(user, expirationTime)
}

func (s *Service) GenerateTimeJWT(user models.User, expirationTime time.Time) (string, time.Time, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", time.Now(), err
	}
	return s.generateSessionJWT(user, expirationTime, id.String(), s.Config().JwtSecretKey)
}

func (s *Service) generateSessionJWT(user models.User, expirationTime time.Time, sessionID, signingKey string) (string, time.Time, error) {
	key := []byte(signingKey)
	// Create the JWT claims, which includes the username and expiry time
	claims := &Claims{
		UserID:         user.ID,
		Username:       user.Username,
		Admin:          user.Admin,
		SessionVersion: user.SessionVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       sessionID,
			IssuedAt: jwt.NewNumericDate(time.Now()),
			// In JWT, the expiry time is expressed as unix milliseconds
			ExpiresAt: jwt.NewNumericDate(expirationTime),
		},
	}

	// Declare the token with the algorithm used for signing, and the claims
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	// Create the JWT string
	tokenString, err := token.SignedString(key)
	if err != nil {
		return "", time.Now(), err
	}
	return tokenString, expirationTime, nil
}

func (s *Service) VerifyJWT(tknStr string) (*jwt.Token, *Claims, error) {
	return s.verifyJWTWithKey(tknStr, s.Config().JwtSecretKey)
}

func (s *Service) verifyJWTWithKey(tknStr, signingKey string) (*jwt.Token, *Claims, error) {
	tkn, claims, err := s.verifyJWTIdentity(tknStr, signingKey)
	if err != nil {
		return nil, nil, err
	}
	var revoked int64
	if err := s.Deps.DB.Model(&models.RevokedAuthSession{}).
		Where("session_id = ? AND user_id = ?", sessionIdentifier(claims, tknStr), claims.UserID).Count(&revoked).Error; err != nil {
		return nil, nil, err
	}
	if revoked != 0 {
		return nil, nil, errors.New("jwt session revoked")
	}
	return tkn, claims, nil
}

// verifyJWTIdentity validates the signature, expiry and live account state.
// API access and refresh also require the revocation check above. Logout may
// accept an already-revoked session so a lost response can be retried safely.
func (s *Service) verifyJWTIdentity(tknStr, signingKey string) (*jwt.Token, *Claims, error) {
	key := []byte(signingKey)
	claims := &Claims{}

	// Parse the JWT string and store the result in `claims`.
	// Note that we are passing the key in this method as well. This method will return an error
	// if the token is invalid (if it has expired according to the expiry time we set on sign in),
	// or if the signature does not match
	tkn, err := jwt.ParseWithClaims(tknStr, claims, func(token *jwt.Token) (interface{}, error) {
		return key, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil {
		return nil, nil, err
	}
	if !tkn.Valid || claims.UserID == 0 {
		return nil, nil, errors.New("invalid jwt key")
	}
	if s == nil || s.Deps == nil || s.Deps.DB == nil {
		return nil, nil, errors.New("database dependency is nil")
	}
	var user models.User
	if err := s.Deps.DB.First(&user, claims.UserID).Error; err != nil {
		return nil, nil, err
	}
	// Missing versions in legacy tokens decode as zero, which remains valid
	// only until the account's first password or privilege change.
	if claims.SessionVersion != user.SessionVersion {
		return nil, nil, errors.New("jwt session revoked")
	}
	claims.Username = user.Username
	claims.Admin = user.Admin
	return tkn, claims, nil
}

func (s *Service) RefreshJWT(tknStr string) (string, time.Time, error) {
	// Verification and reissuance must use the same key. A concurrent rotation
	// then invalidates this result instead of upgrading an old session's key.
	signingKey := s.Config().JwtSecretKey
	_, claims, err := s.verifyJWTWithKey(tknStr, signingKey)
	if err != nil {
		return "", time.Now(), errors.New("Malformated jwt key")
	}

	// We ensure that a new token is not issued until enough time has elapsed
	// In this case, a new token will only be issued if the old token is within
	// five minutes of expiry. Otherwise, return a bad request status
	if time.Until(claims.ExpiresAt.Time) > 5*time.Minute {
		return "", time.Now(), fmt.Errorf("Wait until time to expire: %v", claims.ExpiresAt.Time.String())
	}

	return s.generateSessionJWT(models.User{
		Model: models.Model{ID: claims.UserID}, Username: claims.Username,
		Admin: claims.Admin, SessionVersion: claims.SessionVersion,
	}, time.Now().Add(sessionDuration), sessionIdentifier(claims, tknStr), signingKey)
}

func sessionIdentifier(claims *Claims, token string) string {
	if claims.ID != "" {
		return claims.ID
	}
	// The signature's base64url text can have equivalent encodings. Identify
	// legacy sessions by the immutable signed header and claims instead.
	signedInput := token[:strings.LastIndexByte(token, '.')]
	digest := sha256.Sum256([]byte(signedInput))
	return "legacy:" + hex.EncodeToString(digest[:])
}

func (s *Service) RevokeJWT(token string) error {
	_, claims, err := s.verifyJWTIdentity(token, s.Config().JwtSecretKey)
	if err != nil {
		return err
	}
	return s.Deps.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.RevokedAuthSession{
		SessionID: sessionIdentifier(claims, token), UserID: claims.UserID, RevokedAt: time.Now(),
	}).Error
}
