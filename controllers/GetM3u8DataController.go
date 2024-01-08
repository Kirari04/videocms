package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func GetM3u8Data(c *fiber.Ctx) error {
	var requestValidation logic.GetM3u8DataRequest
	var requestValidationMuted logic.GetM3u8DataRequestMuted
	requestValidation.UUID = c.Params("UUID")
	requestValidation.AUDIOUUID = c.Params("AUDIOUUID")
	requestValidation.JWT = c.Query("jwt", "")
	var JWT string
	if requestValidation.AUDIOUUID != "" {
		// validate audio stream
		if errors := helpers.ValidateStruct(requestValidation); len(errors) > 0 {
			return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
		}
		JWT = requestValidation.JWT
	} else {
		// validate muted stream
		requestValidationMuted.UUID = requestValidation.UUID
		requestValidationMuted.JWT = requestValidation.JWT
		if errors := helpers.ValidateStruct(requestValidationMuted); len(errors) > 0 {
			return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
		}
		JWT = requestValidationMuted.JWT
	}

	status, m3u8Str, err := logic.GetM3u8Data(requestValidation.UUID, requestValidation.AUDIOUUID, JWT)
	if err != nil {
		return c.Status(status).SendString(err.Error())
	}

	return c.Status(status).SendString(*m3u8Str)
}
