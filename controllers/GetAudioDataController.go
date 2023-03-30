package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func GetAudioData(c *fiber.Ctx) error {
	var requestValidation models.AudioGetValidation
	if err := c.ParamsParser(&requestValidation); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(requestValidation); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	status, filePath, err := logic.GetAudioData(&requestValidation)
	if err != nil {
		return c.Status(status).SendString(err.Error())
	}

	if err := c.Status(status).SendFile(*filePath); err != nil {
		return c.Status(fiber.StatusNotFound).SendString("Audio doesn't exist")
	}
	return nil
}
