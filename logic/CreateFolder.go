package logic

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func CreateFolder(folderName string, toFolder uint, userId uint) (status int, newFolder *models.Folder, err error) {
	//check if requested folder exists (if set)
	if toFolder > 0 {
		res := inits.DB.First(&models.Folder{}, toFolder)
		if res.Error != nil {
			return http.StatusBadRequest, nil, errors.New("parent folder doesn't exist")
		}
	}

	// create folder
	folder := models.Folder{
		Name:           folderName,
		ParentFolderID: toFolder,
		UserID:         userId,
	}

	if res := inits.DB.Model(&models.Folder{}).Create(&folder); res.Error != nil {
		log.Printf("Error creating new folder: %v", res.Error)
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}

	return http.StatusOK, &folder, nil
}
