package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func UpdateUserSettingsController(c echo.Context) error {
	// parse & validate request
	var validater models.UserSettingsUpdateValidation
	if status, err := helpers.Validate(c, &validater); err != nil {
		return c.String(status, err.Error())
	}

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

	user.Settings.EnablePlayerCaptcha = *validater.EnablePlayerCaptcha

	if res := inits.DB.Save(&user); res.Error != nil {
		log.Println("Failed to update user settings", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.NoContent(http.StatusOK)
}
