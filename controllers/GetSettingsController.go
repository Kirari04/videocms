package controllers

import (
	"ch/kirari04/videocms/models"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) GetSettings(c echo.Context) error {
	_, ok := c.Get("UserID").(uint)
	if !ok {
		log.Println("Failed to catch user")
		return c.NoContent(http.StatusInternalServerError)
	}

	var setting models.Setting
	if res := h.Deps.DB.FirstOrCreate(&setting); res.Error != nil {
		log.Println("Failed to get settings", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.JSON(http.StatusOK, &setting)
}
