package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"

	"github.com/labstack/echo/v4"
)

func DeleteFolder(c echo.Context) error {
	// parse & validate request
	var folderValidation models.FolderDeleteValidation
	if status, err := helpers.Validate(c, &folderValidation); err != nil {
		return c.String(status, err.Error())
	}

	// Determine admin status
	isAdmin, _ := c.Get("Admin").(bool)

	// Business logic
	status, err := logic.DeleteFolders(&models.FoldersDeleteValidation{
		FolderIDs: []models.FolderDeleteValidation{
			folderValidation,
		},
	}, c.Get("UserID").(uint), isAdmin)
	if err != nil {
		return c.String(status, err.Error())
	}
	return c.NoContent(status)
}
