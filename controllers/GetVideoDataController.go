package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"net/http"

	"github.com/labstack/echo/v4"
)

func GetVideoData(c echo.Context) error {
	type Request struct {
		UUID    string `validate:"required,uuid_rfc4122" param:"UUID"`
		QUALITY string `validate:"required,min=1,max=10" param:"QUALITY"`
		FILE    string `validate:"required" param:"FILE"`
	}
	var requestValidation Request
	if status, err := helpers.Validate(c, &requestValidation); err != nil {
		return c.String(status, err.Error())
	}

	status, filePath, err := logic.GetVideoData(requestValidation.FILE, requestValidation.QUALITY, requestValidation.UUID)
	if err != nil {
		return c.String(status, err.Error())
	}

	if err := c.File(*filePath); err != nil {
		return c.String(http.StatusNotFound, "Video doesn't exist")
	}
	return nil
}
