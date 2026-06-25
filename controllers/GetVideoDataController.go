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

func (h *Handlers) GetVideoData(c echo.Context) error {
	type Request struct {
		UUID    string `validate:"required,uuid_rfc4122" param:"UUID"`
		QUALITY string `validate:"required,min=1,max=10" param:"QUALITY"`
		FILE    string `validate:"required" param:"FILE"`
	}
	var requestValidation Request
	if status, err := helpers.Validate(c, &requestValidation); err != nil {
		return c.String(status, err.Error())
	}

	reQUALITY := regexp.MustCompile(`^([0-9]{3,4}p|(h264))$`)
	reFILE := regexp.MustCompile(`^out[0-9]{0,4}\.(m3u8|ts|webm|mp4)$`)
	if !reQUALITY.MatchString(requestValidation.QUALITY) {
		return c.String(http.StatusBadRequest, "bad quality format")
	}
	if !reFILE.MatchString(requestValidation.FILE) {
		return c.String(http.StatusBadRequest, "bad file format")
	}

	claims, ok := middlewares.MediaClaims(c)
	if !ok {
		return c.String(http.StatusUnauthorized, "Missing media token")
	}
	qualityID, ok := claims.QualityIDs[requestValidation.QUALITY]
	if !ok {
		return c.String(http.StatusNotFound, "Video doesn't exist")
	}

	filePath := fmt.Sprintf("%s/%s/%s/%s", h.Config().FolderVideoQualitysPriv, claims.FileUUID, requestValidation.QUALITY, requestValidation.FILE)
	fileInfo, err := os.Stat(filePath)
	if err == nil {
		h.Logic.TrackTraffic(claims.UserID, claims.FileID, qualityID, 0, uint64(fileInfo.Size()))
	}

	if err := c.File(filePath); err != nil {
		return c.String(http.StatusNotFound, "Video doesn't exist")
	}
	return nil
}
