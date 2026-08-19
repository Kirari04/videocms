package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/middlewares"
	"ch/kirari04/videocms/models"
	"net/http"
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

	if h.Deps.Storage == nil || h.Deps.Storage.Layout() == nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	key, err := h.Deps.Storage.Layout().Audio(claims.FileUUID, requestValidation.AUDIOUUID, requestValidation.FILE)
	if err != nil {
		return c.String(http.StatusBadRequest, "bad media key")
	}
	return h.serveMediaObject(c, claims.StorageID, key, "Audio doesn't exist", mediaTraffic{
		userID:  claims.UserID,
		fileID:  claims.FileID,
		poolID:  claims.StoragePoolID,
		audioID: audioID,
	})
}
