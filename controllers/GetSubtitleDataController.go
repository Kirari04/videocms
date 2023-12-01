package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func GetSubtitleData(c *fiber.Ctx) error {
	type Request struct {
		UUID    string `validate:"required,uuid_rfc4122"`
		SUBUUID string `validate:"required,uuid_rfc4122"`
		FILE    string `validate:"required"`
	}
	var requestValidation Request
	if err := c.ParamsParser(&requestValidation); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(requestValidation); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	status, filePath, err := logic.GetSubtitleData(requestValidation.FILE, requestValidation.UUID, requestValidation.SUBUUID)
	if err != nil {
		return c.Status(status).SendString(err.Error())
	}

	if err := c.SendFile(*filePath); err != nil {
		return c.Status(fiber.StatusNotFound).SendString("Subtitle file not found")
	}
	return nil
}
