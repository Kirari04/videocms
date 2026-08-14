package logic

import (
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/google/uuid"
)

func (s *Service) CloneFileByHash(fromHash string, toFolder uint, fileName string, userId uint, excludeSessionUUID string) (status int, newFile *models.Link, err error) {
	// check if requested folder exists (if set)
	if toFolder > 0 {
		res := s.Deps.DB.First(&models.Folder{}, toFolder)
		if res.Error != nil {
			return http.StatusBadRequest, nil, errors.New("parent folder doesn't exist")
		}
	}

	// Only reuse records whose storage is still available. An unavailable
	// duplicate must fall through to a real upload so the new link is usable.
	var candidates []models.File
	if res := s.Deps.DB.
		Where("hash = ? AND (storage_state IS NULL OR storage_state = '' OR storage_state = ?)", fromHash, models.FileStorageAvailable).
		Order("id ASC").
		Find(&candidates); res.Error != nil || len(candidates) == 0 {
		return http.StatusNotFound, nil, errors.New("requested hash doesnt match any file")
	}
	var existingFile models.File
	var releaseStore func()
	for _, candidate := range candidates {
		release := s.Deps.StorageLifecycle.ReadLock(candidate.StorageID)
		var current models.File
		if err := s.Deps.DB.
			Where("id = ? AND (storage_state IS NULL OR storage_state = '' OR storage_state = ?)", candidate.ID, models.FileStorageAvailable).
			First(&current).Error; err != nil {
			release()
			continue
		}
		if s.Deps.Storage == nil {
			release()
			continue
		}
		if _, err := s.Deps.Storage.StoreOrDefault(current.StorageID); err != nil {
			release()
			continue
		}
		existingFile = current
		releaseStore = release
		break
	}
	if releaseStore == nil {
		return http.StatusNotFound, nil, errors.New("requested hash doesnt match any available file")
	}
	defer releaseStore()

	// check storage quota
	if status, err := s.CheckStorageQuota(userId, existingFile.Size, excludeSessionUUID); err != nil {
		return status, nil, err
	}

	// file is dublicate and can be linked
	// link old uploaded file to new link
	dbLink := models.Link{
		UUID:           uuid.NewString(),
		CreationKey:    uploadCreationKey(userId, excludeSessionUUID),
		ParentFolderID: toFolder,
		UserID:         userId,
		FileID:         existingFile.ID,
		Name:           fileName,
	}
	if res := s.Deps.DB.Create(&dbLink); res.Error != nil {
		log.Printf("Error saving link in database: %v", res.Error)
		return http.StatusInternalServerError, nil, res.Error
	}
	return http.StatusOK, &dbLink, nil
}

func uploadCreationKey(userID uint, clientUploadUUID string) string {
	if clientUploadUUID == "" {
		return ""
	}
	return fmt.Sprintf("upload:%d:%s", userID, clientUploadUUID)
}
