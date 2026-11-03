package logic

import (
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (s *Service) CloneFileByHash(fromHash string, toFolder uint, fileName string, userId uint, excludeSessionUUID string) (status int, newFile *models.Link, err error) {
	if status, err := ValidateFolderDestination(s.Deps.DB, toFolder, userId); err != nil {
		return status, nil, err
	}

	// Only reuse records whose storage is still available. An unavailable
	// duplicate must fall through to a real upload so the new link is usable.
	var candidates []models.File
	if res := s.Deps.DB.
		Where("hash = ? AND (storage_state IS NULL OR storage_state = '' OR storage_state = ?)", fromHash, models.FileStorageAvailable).
		Order("id ASC").
		Find(&candidates); res.Error != nil {
		return http.StatusInternalServerError, nil, res.Error
	}
	if len(candidates) == 0 {
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
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return http.StatusInternalServerError, nil, err
			}
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
	status = http.StatusInternalServerError
	if err := s.Deps.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if status, err = ValidateFolderDestination(tx, toFolder, userId); err != nil {
			return err
		}
		status = http.StatusInternalServerError
		return tx.Create(&dbLink).Error
	}); err != nil {
		log.Printf("Error saving link in database: %v", err)
		return status, nil, err
	}
	return http.StatusOK, &dbLink, nil
}

func uploadCreationKey(userID uint, clientUploadUUID string) string {
	if clientUploadUUID == "" {
		return ""
	}
	return fmt.Sprintf("upload:%d:%s", userID, clientUploadUUID)
}
