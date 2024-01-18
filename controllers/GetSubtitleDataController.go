package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"net/http"

	"github.com/labstack/echo/v4"
)

func GetSubtitleData(c echo.Context) error {
	type Request struct {
		UUID    string `validate:"required,uuid_rfc4122"`
		SUBUUID string `validate:"required,uuid_rfc4122"`
		FILE    string `validate:"required"`
	}
	var requestValidation Request
	if status, err := helpers.Validate(c, &requestValidation); err != nil {
		return c.String(status, err.Error())
	}

	status, filePath, err := logic.GetSubtitleData(requestValidation.FILE, requestValidation.UUID, requestValidation.SUBUUID)
	if err != nil {
		return c.String(status, err.Error())
	}

	if err := c.File(*filePath); err != nil {
		return c.String(http.StatusNotFound, "Subtitle file not found")
	}
	return nil
}
