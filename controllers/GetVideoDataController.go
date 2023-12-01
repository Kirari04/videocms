package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func GetVideoData(c *fiber.Ctx) error {
	type Request struct {
		UUID    string `validate:"required,uuid_rfc4122"`
		QUALITY string `validate:"required,min=1,max=10"`
		FILE    string `validate:"required"`
	}
	var requestValidation Request
	if err := c.ParamsParser(&requestValidation); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(requestValidation); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	status, filePath, err := logic.GetVideoData(requestValidation.FILE, requestValidation.QUALITY, requestValidation.UUID)
	if err != nil {
		return c.Status(status).SendString(err.Error())
	}

	if err := c.Status(status).SendFile(*filePath); err != nil {
		return c.Status(fiber.StatusNotFound).SendString("Video doesn't exist")
	}
	return nil
}
