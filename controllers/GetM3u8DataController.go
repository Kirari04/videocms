package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
)

func GetM3u8Data(c echo.Context) error {
	var requestValidation logic.GetM3u8DataRequest
	var requestValidationMuted logic.GetM3u8DataRequestMuted
	requestValidation.UUID = c.Param("UUID")
	requestValidation.AUDIOUUID = c.Param("AUDIOUUID")
	requestValidation.JWT = c.QueryParam("jwt")
	var JWT string
	if requestValidation.AUDIOUUID != "" {
		// validate audio stream
		if errors := helpers.ValidateStruct(requestValidation); len(errors) > 0 {
			return c.String(http.StatusBadRequest, fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
		}
		JWT = requestValidation.JWT
	} else {
		// validate muted stream
		requestValidationMuted.UUID = requestValidation.UUID
		requestValidationMuted.JWT = requestValidation.JWT
		if errors := helpers.ValidateStruct(requestValidationMuted); len(errors) > 0 {
			return c.String(http.StatusBadRequest, fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
		}
		JWT = requestValidationMuted.JWT
	}

	status, m3u8Str, err := logic.GetM3u8Data(requestValidation.UUID, requestValidation.AUDIOUUID, JWT)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.String(status, *m3u8Str)
}

func GetM3u8DataMulti(c echo.Context) error {
	var requestValidationMuted logic.GetM3u8DataRequestMuted
	requestValidationMuted.UUID = c.Param("UUID")
	requestValidationMuted.JWT = c.QueryParam("jwt")

	if errors := helpers.ValidateStruct(requestValidationMuted); len(errors) > 0 {
		return c.String(http.StatusBadRequest, fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	status, m3u8Str, err := logic.GetM3u8DataMulti(requestValidationMuted.UUID, requestValidationMuted.JWT)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.String(status, *m3u8Str)
}
