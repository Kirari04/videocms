package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/middlewares"
	"net/http"
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

	if h.Deps.Storage == nil || h.Deps.Storage.Layout() == nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	key, err := h.Deps.Storage.Layout().Subtitle(claims.FileUUID, requestValidation.SUBUUID, requestValidation.FILE)
	if err != nil {
		return c.String(http.StatusBadRequest, "bad media key")
	}
	return h.serveMediaObject(c, claims.StorageID, key, "Subtitle file not found", mediaTraffic{
		userID: claims.UserID,
		fileID: claims.FileID,
		poolID: claims.StoragePoolID,
	})
}

func subtitleAllowed(subtitleUUIDs []string, subtitleUUID string) bool {
	for _, allowed := range subtitleUUIDs {
		if allowed == subtitleUUID {
			return true
		}
	}
	return false
}
