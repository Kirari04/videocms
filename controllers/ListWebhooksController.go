package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"
)

func ListWebhooks(c *fiber.Ctx) error {
	// parse & validate request
	var validation models.WebhookListValidation
	if err := c.QueryParser(&validation); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(validation); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	userID := c.Locals("UserID").(uint)

	var dataList []models.Webhook
	if res := inits.DB.Where(&models.Webhook{
		UserID: userID,
	}).Find(&dataList); res.Error != nil {
		log.Printf("Failed to fetch webhooks from database: %v", res.Error)
		return c.Status(fiber.StatusInternalServerError).SendString(fiber.ErrInternalServerError.Message)
	}

	return c.JSON(dataList)
}
