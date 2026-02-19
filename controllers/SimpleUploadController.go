package controllers

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
)

func SimpleUploadController(c echo.Context) error {
	// check if uploads are enabled
	if !*config.ENV.UploadEnabled {
		return c.String(http.StatusForbidden, "Uploads are disabled")
	}

	userID, ok := c.Get("UserID").(uint)
	if !ok {
		return c.String(http.StatusInternalServerError, "Failed to catch UserID")
	}

	// parse & validate request
	var validation models.SimpleUploadValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}

	// file processing
	file, err := c.FormFile("file")
	if err != nil {
		return c.String(http.StatusBadRequest, "No file uploaded")
	}

	// size check
	if file.Size > config.ENV.MaxUploadFilesize {
		return c.String(http.StatusRequestEntityTooLarge, fmt.Sprintf("Exceeded max upload filesize: %v", config.ENV.MaxUploadFilesize))
	}

	src, err := file.Open()
	if err != nil {
		c.Logger().Error("Failed to open uploaded file", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	defer src.Close()

	// Business logic
	status, response, err := logic.SimpleUpload(
		validation.ParentFolderID,
		validation.Name,
		src,
		file.Size,
		userID,
	)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(status, response)
}
