package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func CreateWebPage(c echo.Context) error {
	// parse & validate request
	var validatus models.WebPageCreateValidation
	if status, err := helpers.Validate(c, &validatus); err != nil {
		return c.String(status, err.Error())
	}

	var existing int64
	if res := inits.DB.Model(&models.WebPage{}).Where(&models.WebPage{
		Path: validatus.Path,
	}).Count(&existing); res.Error != nil {
		c.Logger().Error("Failed to count webpage path", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}
	if existing > 0 {
		return c.String(http.StatusBadRequest, "Path already used")
	}

	if validatus.Path[len(validatus.Path)-1] != '/' {
		validatus.Path = fmt.Sprintf("%s/", validatus.Path)
	}

	webPage := models.WebPage{
		Path:         validatus.Path,
		Title:        validatus.Title,
		Html:         validatus.Html,
		ListInFooter: *validatus.ListInFooter,
	}
	if res := inits.DB.Create(&webPage); res.Error != nil {
		log.Println("Failed to create webpage", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.String(http.StatusOK, "ok")
}
