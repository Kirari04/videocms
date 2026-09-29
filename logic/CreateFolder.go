package logic

import (
	"ch/kirari04/videocms/models"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (s *Service) CreateFolder(folderName string, toFolder uint, userId uint) (status int, newFolder *models.Folder, err error) {
	// create folder
	folder := models.Folder{
		Name:           folderName,
		ParentFolderID: toFolder,
		UserID:         userId,
	}

	status = http.StatusInternalServerError
	if err := s.Deps.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if status, err = ValidateFolderDestination(tx, toFolder, userId); err != nil {
			return err
		}
		status = http.StatusInternalServerError
		return tx.Create(&folder).Error
	}); err != nil {
		if status != http.StatusInternalServerError {
			return status, nil, err
		}
		log.Printf("Error creating new folder: %v", err)
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}

	return http.StatusOK, &folder, nil
}
