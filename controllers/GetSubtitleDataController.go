package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/middlewares"
	"fmt"
	"net/http"
	"os"
	"regexp"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) GetSubtitleData(c echo.Context) error {
	type Request struct {
		UUID    string `validate:"required,uuid_rfc4122" param:"UUID"`
		SUBUUID string `validate:"required,uuid_rfc4122" param:"SUBUUID"`
		FILE    string `validate:"required" param:"FILE"`
	}
	var requestValidation Request
	if status, err := helpers.Validate(c, &requestValidation); err != nil {
		return c.String(status, err.Error())
	}

	reFILE := regexp.MustCompile(`^out\.(ass|vtt)$`)
	if !reFILE.MatchString(requestValidation.FILE) {
		return c.String(http.StatusBadRequest, "bad file format")
	}

	claims, ok := middlewares.MediaClaims(c)
	if !ok {
		return c.String(http.StatusUnauthorized, "Missing media token")
	}
	if !subtitleAllowed(claims.SubtitleUUIDs, requestValidation.SUBUUID) {
		return c.String(http.StatusNotFound, "Subtitle doesn't exist")
	}

	filePath := fmt.Sprintf("%s/%s/%s/%s", h.Config().FolderVideoQualitysPriv, claims.FileUUID, requestValidation.SUBUUID, requestValidation.FILE)
	fileInfo, err := os.Stat(filePath)
	if err == nil {
		h.Logic.TrackTraffic(claims.UserID, claims.FileID, 0, 0, uint64(fileInfo.Size()))
	}

	if err := c.File(filePath); err != nil {
		return c.String(http.StatusNotFound, "Subtitle file not found")
	}
	return nil
}

func subtitleAllowed(subtitleUUIDs []string, subtitleUUID string) bool {
	for _, allowed := range subtitleUUIDs {
		if allowed == subtitleUUID {
			return true
		}
	}
	return false
}
