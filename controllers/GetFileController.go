package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"

	"github.com/labstack/echo/v4"
)

func GetFile(c echo.Context) error {
	// parse & validate request
	var fileValidation models.LinkGetValidation
	if status, err := helpers.Validate(c, &fileValidation); err != nil {
		return c.String(status, err.Error())
	}

	// Determine admin status
	isAdmin, _ := c.Get("Admin").(bool)

	// Business logic
	status, response, err := logic.GetFile(fileValidation.LinkID, c.Get("UserID").(uint), isAdmin)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(status, response)
}
