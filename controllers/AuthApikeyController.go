package controllers

import (
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

func AuthApikey(c echo.Context) error {
	userId, ok := c.Get("UserID").(uint)
	if !ok {
		c.Logger().Error("Failed to catch user")
		return c.NoContent(http.StatusInternalServerError)
	}

	var user models.User
	res := inits.DB.
		Model(&models.User{}).
		First(&user, userId)
	if res.Error != nil {
		return c.String(http.StatusBadRequest, "User not found")
	}
	expirationTime := time.Now().Add(time.Hour * 24 * 365)
	tokenString, _, err := auth.GenerateTimeJWT(user, expirationTime)
	if err != nil {
		log.Printf("Failed to generate jwt for user %s: %v\n", user.Username, err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, echo.Map{
		"exp":   expirationTime,
		"token": tokenString,
	})
}
