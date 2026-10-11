package auth

import (
	"errors"
	"time"

	"ch/kirari04/videocms/models"
	"github.com/golang-jwt/jwt/v5"
)

const folderShareAudience = "videocms-folder-share"

type FolderShareClaims struct {
	AccessVersion string `json:"access_version"`
	jwt.RegisteredClaims
}

func FolderShareCookieName(token string) string { return "vc_share_" + token }

func (s *Service) GenerateFolderShareToken(share *models.FolderShare) (string, time.Time, error) {
	if s.Config().JwtSecretKey == "" {
		return "", time.Time{}, errors.New("share secret is empty")
	}
	expires := time.Now().Add(6 * time.Hour)
	if share.ExpiresAt != nil && share.ExpiresAt.Before(expires) {
		expires = *share.ExpiresAt
	}
	claims := FolderShareClaims{
		AccessVersion: share.AccessVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: share.Token, Audience: jwt.ClaimStrings{folderShareAudience},
			ExpiresAt: jwt.NewNumericDate(expires), IssuedAt: jwt.NewNumericDate(time.Now()),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.Config().JwtSecretKey + "_folder_share"))
	return token, expires, err
}

func (s *Service) VerifyFolderShareToken(token string, share *models.FolderShare) bool {
	if s.Config().JwtSecretKey == "" {
		return false
	}
	claims := &FolderShareClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(_ *jwt.Token) (any, error) {
		return []byte(s.Config().JwtSecretKey + "_folder_share"), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithAudience(folderShareAudience), jwt.WithExpirationRequired())
	return err == nil && parsed.Valid && claims.Subject == share.Token && claims.AccessVersion == share.AccessVersion
}
