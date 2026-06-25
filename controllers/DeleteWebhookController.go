package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/models"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) DeleteWebhook(c echo.Context) error {
	// parse & validate request
	var validation models.WebhookDeleteValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}

	userID := c.Get("UserID").(uint)

	status, response, err := h.Logic.DeleteWebhook(&validation, userID)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.String(status, response)
}
