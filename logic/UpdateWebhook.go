package logic

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"log"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

func UpdateWebhook(validated *models.WebhookUpdateValidation, userID uint) (status int, response string, err error) {

	var webhook models.Webhook
	if res := inits.DB.Where(&models.Webhook{
		UserID: userID,
	}).First(&webhook, validated.WebhookID); res.Error != nil {
		if errors.Is(res.Error, gorm.ErrRecordNotFound) {
			return fiber.StatusNotFound, "", fiber.ErrNotFound
		}
		log.Printf("Failed to query webhook %v: %v", validated.WebhookID, res.Error)
		return fiber.StatusInternalServerError, "", fiber.ErrInternalServerError
	}

	webhook.Name = validated.Name
	webhook.Url = validated.Url
	webhook.Rpm = validated.Rpm
	webhook.ReqQuery = validated.ReqQuery
	webhook.ResField = validated.ResField

	if res := inits.DB.Save(&webhook); res.Error != nil {
		log.Printf("Failed to update webhook %v: %v", validated.WebhookID, res.Error)
		return fiber.StatusInternalServerError, "", fiber.ErrInternalServerError
	}

	return fiber.StatusOK, "ok", nil
}
