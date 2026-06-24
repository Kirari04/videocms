package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/middlewares"
	"ch/kirari04/videocms/models"
	"fmt"
	"net/http"
	"os"
	"regexp"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) GetAudioData(c echo.Context) error {
	var requestValidation models.AudioGetValidation
	if status, err := helpers.Validate(c, &requestValidation); err != nil {
		return c.String(status, err.Error())
	}

	reFILE := regexp.MustCompile(`^audio[0-9]{0,4}\.(m3u8|ts|wav|mp3|ogg)$`)
	if !reFILE.MatchString(requestValidation.FILE) {
		return c.String(http.StatusBadRequest, "bad file format")
	}

	claims, ok := middlewares.MediaClaims(c)
	if !ok {
		return c.String(http.StatusUnauthorized, "Missing media token")
	}
	audioID, ok := claims.AudioIDs[requestValidation.AUDIOUUID]
	if !ok {
		return c.String(http.StatusNotFound, "Audio doesn't exist")
	}

	filePath := fmt.Sprintf("%s/%s/%s/%s", h.Config().FolderVideoQualitysPriv, claims.FileUUID, requestValidation.AUDIOUUID, requestValidation.FILE)
	fileInfo, err := os.Stat(filePath)
	if err == nil {
		h.Logic.TrackTraffic(claims.UserID, claims.FileID, 0, audioID, uint64(fileInfo.Size()))
	}

	if err := c.File(filePath); err != nil {
		return c.String(http.StatusNotFound, "Audio doesn't exist")
	}
	return nil
}
