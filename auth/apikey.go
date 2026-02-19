package auth

import (
	"ch/kirari04/videocms/models"
	"errors"
	"time"

	"gorm.io/gorm"
)

func VerifyApiKey(db *gorm.DB, key string) (*models.ApiKey, error) {
	var apiKey models.ApiKey
	if err := db.Preload("User").Where("`key` = ?", key).First(&apiKey).Error; err != nil {
		return nil, err
	}

	if apiKey.ExpiresAt != nil && time.Now().After(*apiKey.ExpiresAt) {
		return nil, errors.New("api key expired")
	}

	return &apiKey, nil
}
