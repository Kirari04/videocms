package logic

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func CreateWebhook(webhookValidation *models.WebhookCreateValidation, userID uint) (status int, response string, err error) {
	if res := inits.DB.Create(&models.Webhook{
		Name:     webhookValidation.Name,
		Url:      webhookValidation.Url,
		Rpm:      webhookValidation.Rpm,
		ReqQuery: webhookValidation.ReqQuery,
		ResField: webhookValidation.ResField,
		UserID:   userID,
	}); res.Error != nil {
		log.Printf("Failed to create webhook: %v", res.Error)
		return http.StatusInternalServerError, "", echo.ErrInternalServerError
	}
	return http.StatusOK, "ok", nil
}
