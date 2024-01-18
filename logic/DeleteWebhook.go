package logic

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func DeleteWebhook(validated *models.WebhookDeleteValidation, userID uint) (status int, response string, err error) {

	var webhook models.Webhook
	if res := inits.DB.Where(&models.Webhook{
		UserID: userID,
	}).First(&webhook, validated.WebhookID); res.Error != nil {
		if errors.Is(res.Error, gorm.ErrRecordNotFound) {
			return http.StatusNotFound, "", echo.ErrNotFound
		}
		log.Printf("Failed to query webhook %v: %v", validated.WebhookID, res.Error)
		return http.StatusInternalServerError, "", echo.ErrInternalServerError
	}
	if res := inits.DB.Delete(&webhook); res.Error != nil {
		log.Printf("Failed to delete webhook %v: %v", validated.WebhookID, res.Error)
		return http.StatusInternalServerError, "", echo.ErrInternalServerError
	}

	return http.StatusOK, "ok", nil
}
