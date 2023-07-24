package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func UpdateWebhook(c *fiber.Ctx) error {
	// parse & validate request
	var validation models.WebhookUpdateValidation
	if err := c.BodyParser(&validation); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(validation); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	userID := c.Locals("UserID").(uint)

	status, response, err := logic.UpdateWebhook(&validation, userID)
	if err != nil {
		return c.Status(status).SendString(err.Error())
	}

	return c.Status(status).SendString(response)
}
