package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/models"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handlers) GetPublicWebPage(c echo.Context) error {
	// parse & validate request
	var validatus models.WebPageGetValidation
	if status, err := helpers.Validate(c, &validatus); err != nil {
		return c.String(status, err.Error())
	}

	var webPage models.WebPage
	if res := h.Deps.DB.
		Where(&models.WebPage{
			Path: validatus.Path,
		}).
		First(&webPage); res.Error != nil {
		if errors.Is(res.Error, gorm.ErrRecordNotFound) {
			return c.String(http.StatusNotFound, "Page not found")
		}
		c.Logger().Error("Failed to get webpage", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.String(http.StatusOK, webPage.Html)
}
