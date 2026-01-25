package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
)

func GetSubtitleData(c echo.Context) error {
	type Request struct {
		UUID    string `validate:"required,uuid_rfc4122" param:"UUID"`
		SUBUUID string `validate:"required,uuid_rfc4122" param:"SUBUUID"`
		FILE    string `validate:"required" param:"FILE"`
	}
	var requestValidation Request
	if status, err := helpers.Validate(c, &requestValidation); err != nil {
		return c.String(status, err.Error())
	}

	status, filePath, userID, fileID, err := logic.GetSubtitleData(requestValidation.FILE, requestValidation.UUID, requestValidation.SUBUUID)
	if err != nil {
		return c.String(status, err.Error())
	}

	fileInfo, err := os.Stat(*filePath)
	if err == nil {
		helpers.TrackTraffic(userID, fileID, 0, 0, uint64(fileInfo.Size()))
	}

	if err := c.File(*filePath); err != nil {
		return c.String(http.StatusNotFound, "Subtitle file not found")
	}
	return nil
}
