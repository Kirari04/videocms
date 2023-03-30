package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func DeleteFilesController(c *fiber.Ctx) error {
	// parse & validate request
	var fileValidation models.LinksDeleteValidation
	if err := c.BodyParser(&fileValidation); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}
	if errors := helpers.ValidateStruct(fileValidation); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	// Business logic
	status, err := logic.DeleteFiles(&fileValidation, c.Locals("UserID").(uint))

	return c.Status(status).SendString(err.Error())
}
