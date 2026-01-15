package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"

	"github.com/labstack/echo/v4"
)

func SearchFiles(c echo.Context) error {
	// parse & validate request
	var searchValidation models.LinkSearchValidation
	if status, err := helpers.Validate(c, &searchValidation); err != nil {
		return c.String(status, err.Error())
	}

	// Determine which UserID to use
	userID := c.Get("UserID").(uint)
	isAdmin, _ := c.Get("Admin").(bool)

	if isAdmin && searchValidation.UserID > 0 {
		userID = searchValidation.UserID
	}

	status, response, err := logic.SearchFiles(userID, searchValidation.Query)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(status, response)
}
