package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func GetUserSettingsController(c echo.Context) error {
	userId, ok := c.Get("UserID").(uint)
	if !ok {
		log.Println("Failed to catch userID")
		return c.NoContent(http.StatusInternalServerError)
	}

	var user models.User
	if res := inits.DB.First(&user, userId); res.Error != nil {
		log.Println("Failed to catch userID on db")
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, echo.Map{
		"EnablePlayerCaptcha": user.Settings.EnablePlayerCaptcha,
	})
}
