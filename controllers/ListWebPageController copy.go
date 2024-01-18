package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func ListWebPage(c echo.Context) error {
	var webPages []models.WebPage
	if res := inits.DB.Find(&webPages); res.Error != nil {
		log.Println("Failed to list webpages", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.JSON(http.StatusOK, &webPages)
}
