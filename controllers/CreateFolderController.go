package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"

	"github.com/labstack/echo/v4"
)

func CreateFolder(c echo.Context) error {
	// parse & validate request
	var folderValidation models.FolderCreateValidation
	if status, err := helpers.Validate(c, &folderValidation); err != nil {
		return c.String(status, err.Error())
	}

	status, dbFolder, err := logic.CreateFolder(folderValidation.Name, folderValidation.ParentFolderID, c.Get("UserID").(uint))

	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(status, dbFolder)
}
