package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func UpdateWebPage(c echo.Context) error {
	// parse & validate request
	var validatus models.WebPageUpdateValidation
	if status, err := helpers.Validate(c, &validatus); err != nil {
		return c.String(status, err.Error())
	}

	var existing int64
	if res := inits.DB.Model(&models.WebPage{}).
		Where("id != ?", validatus.WebPageID).
		Where("path = ?", validatus.Path).
		Count(&existing); res.Error != nil {
		log.Println("Failed to count webpage path", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}
	if existing > 0 {
		return c.String(http.StatusBadRequest, "Path already used")
	}

	var webPage models.WebPage
	if res := inits.DB.First(&webPage, validatus.WebPageID); res.Error != nil {
		if errors.Is(res.Error, gorm.ErrRecordNotFound) {
			return c.String(http.StatusNotFound, "Webpage not found")
		}
		log.Println("Failed to find webpage", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}

	webPage.Path = validatus.Path
	webPage.Title = validatus.Title
	webPage.Html = validatus.Html
	webPage.ListInFooter = *validatus.ListInFooter

	if res := inits.DB.Save(&webPage); res.Error != nil {
		log.Println("Failed to update webpage", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.String(http.StatusOK, "ok")
}
