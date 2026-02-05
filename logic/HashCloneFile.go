package logic

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"log"
	"net/http"

	"github.com/google/uuid"
)

func CloneFileByHash(fromHash string, toFolder uint, fileName string, userId uint, excludeSessionUUID string) (status int, newFile *models.Link, err error) {
	// check if requested folder exists (if set)
	if toFolder > 0 {
		res := inits.DB.First(&models.Folder{}, toFolder)
		if res.Error != nil {
			return http.StatusBadRequest, nil, errors.New("parent folder doesn't exist")
		}
	}

	// check file hash with database
	var existingFile models.File
	if res := inits.DB.
		Where(&models.File{
			Hash: fromHash,
		}).First(&existingFile); res.Error != nil {
		return http.StatusNotFound, nil, errors.New("requested hash doesnt match any file")
	}

	// check storage quota
	if status, err := CheckStorageQuota(userId, existingFile.Size, excludeSessionUUID); err != nil {
		return status, nil, err
	}

	// file is dublicate and can be linked
	// link old uploaded file to new link
	dbLink := models.Link{
		UUID:           uuid.NewString(),
		ParentFolderID: toFolder,
		UserID:         userId,
		FileID:         existingFile.ID,
		Name:           fileName,
	}
	if res := inits.DB.Create(&dbLink); res.Error != nil {
		log.Printf("Error saving link in database: %v", res.Error)
		return http.StatusInternalServerError, nil, res.Error
	}
	return http.StatusOK, &dbLink, nil
}
