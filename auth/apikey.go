package auth

import (
	"ch/kirari04/videocms/models"
	"errors"
	"time"
)

func (s *Service) VerifyApiKey(key string) (*models.ApiKey, error) {
	if s == nil || s.Deps == nil || s.Deps.DB == nil {
		return nil, errors.New("database dependency is nil")
	}

	var apiKey models.ApiKey
	if err := s.Deps.DB.Preload("User").Where("`key` = ?", key).First(&apiKey).Error; err != nil {
		return nil, err
	}

	if apiKey.ExpiresAt != nil && time.Now().After(*apiKey.ExpiresAt) {
		return nil, errors.New("api key expired")
	}

	return &apiKey, nil
}
