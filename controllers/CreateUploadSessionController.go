package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func CreateUploadSession(c echo.Context) error {
	// parse & validate request
	var validation models.UploadSessionValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}

	// business logic
	uploadSessionUUID := uuid.NewString()
	status, response, err := logic.CreateUploadSession(
		validation.ParentFolderID,
		validation.Name,
		uploadSessionUUID,
		validation.Size,
		c.Get("UserID").(uint),
	)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(status, response)
}
