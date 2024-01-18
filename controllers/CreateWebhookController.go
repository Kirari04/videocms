package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"

	"github.com/labstack/echo/v4"
)

func CreateWebhook(c echo.Context) error {
	// parse & validate request

	var webhookValidation models.WebhookCreateValidation
	if status, err := helpers.Validate(c, &webhookValidation); err != nil {
		return c.String(status, err.Error())
	}

	status, res, err := logic.CreateWebhook(&webhookValidation, c.Get("UserID").(uint))

	if err != nil {
		return c.String(status, err.Error())
	}

	return c.String(status, res)
}
