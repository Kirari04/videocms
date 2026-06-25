package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const MediaCookieName = "vc_media"
const MediaTokenDuration = 6 * time.Hour

const mediaAudience = "videocms-media"

type MediaClaims struct {
	LinkUUID      string          `json:"link_uuid"`
	FileUUID      string          `json:"file_uuid"`
	UserID        uint            `json:"user_id"`
	FileID        uint            `json:"file_id"`
	QualityIDs    map[string]uint `json:"quality_ids"`
	AudioIDs      map[string]uint `json:"audio_ids"`
	SubtitleUUIDs []string        `json:"subtitle_uuids"`
	jwt.RegisteredClaims
}

func (s *Service) GenerateMediaToken(claims MediaClaims) (string, time.Time, error) {
	mediaKey := []byte(s.Config().JwtMediaSecretKey)
	if len(mediaKey) == 0 {
		return "", time.Now(), errors.New("media secret key is empty")
	}

	expirationTime := time.Now().Add(MediaTokenDuration)
	claims.RegisteredClaims = jwt.RegisteredClaims{
		Subject:   claims.LinkUUID,
		Audience:  jwt.ClaimStrings{mediaAudience},
		ExpiresAt: jwt.NewNumericDate(expirationTime),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(mediaKey)
	if err != nil {
		return "", time.Now(), err
	}
	return tokenString, expirationTime, nil
}

func (s *Service) VerifyMediaToken(tknStr string) (*jwt.Token, *MediaClaims, error) {
	mediaKey := []byte(s.Config().JwtMediaSecretKey)
	if len(mediaKey) == 0 {
		return nil, nil, errors.New("media secret key is empty")
	}

	claims := &MediaClaims{}
	parser := jwt.NewParser(
		jwt.WithAudience(mediaAudience),
		jwt.WithExpirationRequired(),
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	)
	tkn, err := parser.ParseWithClaims(tknStr, claims, func(token *jwt.Token) (interface{}, error) {
		return mediaKey, nil
	})
	if err != nil {
		return nil, nil, err
	}
	if claims.Subject != claims.LinkUUID {
		return nil, nil, errors.New("media token subject mismatch")
	}
	return tkn, claims, nil
}
