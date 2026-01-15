package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func ListFolders(c echo.Context) error {
	// parse & validate request
	var folderValidation models.FolderListValidation
	if status, err := helpers.Validate(c, &folderValidation); err != nil {
		return c.String(status, err.Error())
	}

	//check if requested folder exists
	if folderValidation.ParentFolderID > 0 {
		res := inits.DB.First(&models.Folder{}, folderValidation.ParentFolderID)
		if res.Error != nil {
			return c.String(http.StatusBadRequest, "Parent folder doesn't exist")
		}
	}

	// Determine which UserID to use
	userID := c.Get("UserID").(uint)
	isAdmin, _ := c.Get("Admin").(bool)

	if isAdmin && folderValidation.UserID > 0 {
		userID = folderValidation.UserID
	}

	// query all folders
	var folders []models.Folder
	res := inits.DB.
		Model(&models.Folder{}).
		Preload("User").
		Where(&models.Folder{
			ParentFolderID: folderValidation.ParentFolderID,
			UserID:         userID,
		}, "ParentFolderID", "UserID").
		Order("name ASC").
		Find(&folders)
	if res.Error != nil {
		log.Printf("Failed to query folder list: %v", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}

	// return value
	return c.JSON(http.StatusOK, &folders)
}
