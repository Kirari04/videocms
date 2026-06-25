package logic

import (
	"ch/kirari04/videocms/models"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"time"
)

const ApiKeyPrefix = "ak_"

func (s *Service) GenerateApiKey(userID uint, name string, expiresAt *time.Time) (int, *models.CreateApiKeyResponse, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return http.StatusInternalServerError, nil, err
	}
	key := ApiKeyPrefix + hex.EncodeToString(b)

	apiKey := models.ApiKey{
		UserID:    userID,
		Name:      name,
		Key:       key,
		Prefix:    key[:8] + "...",
		ExpiresAt: expiresAt,
	}

	if err := s.Deps.DB.Create(&apiKey).Error; err != nil {
		return http.StatusInternalServerError, nil, err
	}

	return http.StatusCreated, &models.CreateApiKeyResponse{
		ID:        apiKey.ID,
		Name:      apiKey.Name,
		Key:       apiKey.Key,
		ExpiresAt: apiKey.ExpiresAt,
	}, nil
}

func (s *Service) ListApiKeys(userID uint) (int, []models.ApiKey, error) {
	var apiKeys []models.ApiKey
	if err := s.Deps.DB.Where("user_id = ?", userID).Find(&apiKeys).Error; err != nil {
		return http.StatusInternalServerError, nil, err
	}
	return http.StatusOK, apiKeys, nil
}

func (s *Service) DeleteApiKey(userID uint, keyID uint) (int, error) {
	// Verify ownership before deleting anything
	var count int64
	s.Deps.DB.Model(&models.ApiKey{}).Where("user_id = ? AND id = ?", userID, keyID).Count(&count)
	if count == 0 {
		return http.StatusNotFound, errors.New("api key not found")
	}

	// Delete audit logs first
	if err := s.Deps.DB.Where("api_key_id = ?", keyID).Delete(&models.ApiKeyAuditLog{}).Error; err != nil {
		return http.StatusInternalServerError, err
	}

	// Delete the key
	result := s.Deps.DB.Where("user_id = ? AND id = ?", userID, keyID).Delete(&models.ApiKey{})
	if result.Error != nil {
		return http.StatusInternalServerError, result.Error
	}
	return http.StatusNoContent, nil
}

func (s *Service) GetApiKeyAudit(userID uint, keyID uint) (int, []models.ApiKeyAuditLog, error) {
	var auditLogs []models.ApiKeyAuditLog
	// Verify key belongs to user
	var count int64
	s.Deps.DB.Model(&models.ApiKey{}).Where("id = ? AND user_id = ?", keyID, userID).Count(&count)
	if count == 0 {
		return http.StatusNotFound, nil, errors.New("api key not found")
	}

	if err := s.Deps.DB.Where("api_key_id = ?", keyID).Order("created_at desc").Limit(100).Find(&auditLogs).Error; err != nil {
		return http.StatusInternalServerError, nil, err
	}
	return http.StatusOK, auditLogs, nil
}
