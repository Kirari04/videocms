package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"

	"github.com/labstack/echo/v4"
)

func ListFiles(c echo.Context) error {
	// parse & validate request
	var fileValidation models.LinkListValidation
	if status, err := helpers.Validate(c, &fileValidation); err != nil {
		return c.String(status, err.Error())
	}

	// Determine which UserID to use
	userID := c.Get("UserID").(uint)
	isAdmin, _ := c.Get("Admin").(bool)

	if isAdmin && fileValidation.UserID > 0 {
		userID = fileValidation.UserID
	}

	status, response, err := logic.ListFiles(fileValidation.ParentFolderID, userID)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(status, response)
}
