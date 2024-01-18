package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"

	"github.com/labstack/echo/v4"
)

func CreateUploadFile(c echo.Context) error {
	// parse & validate request
	var validation models.UploadFileValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}

	status, response, err := logic.CreateUploadFile(
		validation.SessionJwtToken,
		c.Get("UserID").(uint),
	)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(status, response)
}
