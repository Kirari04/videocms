package logic

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"log"

	"github.com/gofiber/fiber/v2"
)

func ListFiles(fromFolder uint, userId uint) (status int, response *[]models.Link, err error) {
	//check if requested folder exists
	if fromFolder > 0 {
		res := inits.DB.First(&models.Folder{}, fromFolder)
		if res.Error != nil {
			return fiber.StatusBadRequest, nil, errors.New("parent folder doesn't exist")
		}
	}

	// query all files
	var links []models.Link
	res := inits.DB.
		Model(&models.Link{}).
		Preload("User").
		Preload("File").
		Where(&models.Link{
			ParentFolderID: fromFolder,
			UserID:         userId,
		}, "ParentFolderID", "UserID").
		Find(&links)
	if res.Error != nil {
		log.Printf("Failed to query file list: %v", res.Error)
		return fiber.StatusInternalServerError, nil, errors.New("")
	}

	return fiber.StatusOK, &links, nil
}
