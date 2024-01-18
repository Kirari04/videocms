package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func ListWebhooks(c echo.Context) error {
	// parse & validate request
	var validation models.WebhookListValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}

	userID := c.Get("UserID").(uint)

	var dataList []models.Webhook
	if res := inits.DB.Where(&models.Webhook{
		UserID: userID,
	}).Find(&dataList); res.Error != nil {
		log.Printf("Failed to fetch webhooks from database: %v", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, &dataList)
}
