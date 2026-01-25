package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
)

func GetAudioData(c echo.Context) error {
	var requestValidation models.AudioGetValidation
	if status, err := helpers.Validate(c, &requestValidation); err != nil {
		return c.String(status, err.Error())
	}

	status, filePath, userID, fileID, audioID, err := logic.GetAudioData(&requestValidation)
	if err != nil {
		return c.String(status, err.Error())
	}

	fileInfo, err := os.Stat(*filePath)
	if err == nil {
		helpers.TrackTraffic(userID, fileID, 0, audioID, uint64(fileInfo.Size()))
	}

	if err := c.File(*filePath); err != nil {
		return c.String(http.StatusNotFound, "Audio doesn't exist")
	}
	return nil
}
