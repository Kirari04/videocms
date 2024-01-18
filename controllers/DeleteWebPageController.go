package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func DeleteWebPage(c echo.Context) error {
	// parse & validate request
	var validatus models.WebPageDeleteValidation
	if status, err := helpers.Validate(c, &validatus); err != nil {
		return c.String(status, err.Error())
	}

	res := inits.DB.Delete(&models.WebPage{}, validatus.WebPageID)
	if res.Error != nil {
		log.Println("Failed to delete webpage", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}
	if res.RowsAffected <= 0 {
		return c.String(http.StatusBadRequest, "Webpage not found")
	}

	return c.String(http.StatusOK, "ok")
}
