package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func ListServers(c echo.Context) error {
	var servers []models.Server
	if res := inits.DB.Order("hostname ASC").Find(&servers); res.Error != nil {
		log.Println("Failed to list servers", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, &servers)
}
