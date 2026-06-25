package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) GetM3u8Data(c echo.Context) error {
	var requestValidation logic.GetM3u8DataRequest
	var requestValidationMuted logic.GetM3u8DataRequestMuted
	requestValidation.UUID = c.Param("UUID")
	requestValidation.AUDIOUUID = c.Param("AUDIOUUID")
	if requestValidation.AUDIOUUID != "" {
		// validate audio stream
		if errors := helpers.ValidateStruct(requestValidation); len(errors) > 0 {
			return c.String(http.StatusBadRequest, fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
		}
	} else {
		// validate muted stream
		requestValidationMuted.UUID = requestValidation.UUID
		if errors := helpers.ValidateStruct(requestValidationMuted); len(errors) > 0 {
			return c.String(http.StatusBadRequest, fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
		}
	}

	status, m3u8Str, userID, fileID, audioID, err := h.Logic.GetM3u8Data(requestValidation.UUID, requestValidation.AUDIOUUID)
	if err != nil {
		return c.String(status, err.Error())
	}

	h.Logic.TrackTraffic(userID, fileID, 0, audioID, uint64(len(*m3u8Str)))

	return c.String(status, *m3u8Str)
}

func (h *Handlers) GetM3u8DataMulti(c echo.Context) error {
	var requestValidationMuted logic.GetM3u8DataRequestMuted
	requestValidationMuted.UUID = c.Param("UUID")

	if errors := helpers.ValidateStruct(requestValidationMuted); len(errors) > 0 {
		return c.String(http.StatusBadRequest, fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	status, m3u8Str, userID, fileID, err := h.Logic.GetM3u8DataMulti(requestValidationMuted.UUID)
	if err != nil {
		return c.String(status, err.Error())
	}

	h.Logic.TrackTraffic(userID, fileID, 0, 0, uint64(len(*m3u8Str)))

	return c.String(status, *m3u8Str)
}
