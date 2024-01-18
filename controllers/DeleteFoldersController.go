package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"

	"github.com/labstack/echo/v4"
)

func DeleteFolders(c echo.Context) error {
	// parse & validate request
	var folderValidation models.FoldersDeleteValidation
	if status, err := helpers.Validate(c, &folderValidation); err != nil {
		return c.String(status, err.Error())
	}

	// Business logic
	status, err := logic.DeleteFolders(&folderValidation, c.Get("UserID").(uint))
	if err != nil {
		return c.String(status, err.Error())
	}
	return c.NoContent(status)
}
