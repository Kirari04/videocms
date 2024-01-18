package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"

	"github.com/labstack/echo/v4"
)

func DeleteFilesController(c echo.Context) error {
	// parse & validate request
	var fileValidation models.LinksDeleteValidation
	if status, err := helpers.Validate(c, &fileValidation); err != nil {
		return c.String(status, err.Error())
	}

	// Business logic
	status, err := logic.DeleteFiles(&fileValidation, c.Get("UserID").(uint))

	if err != nil {
		return c.String(status, err.Error())
	}
	return c.NoContent(status)
}
