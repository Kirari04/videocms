package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func GetFile(c *fiber.Ctx) error {
	// parse & validate request
	var fileValidation models.LinkGetValidation
	if err := c.QueryParser(&fileValidation); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(fileValidation); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	// Business logic
	status, response, err := logic.GetFile(fileValidation.LinkID, c.Locals("UserID").(uint))
	if err != nil {
		return c.Status(status).SendString(err.Error())
	}

	return c.Status(status).JSON(response)
}
