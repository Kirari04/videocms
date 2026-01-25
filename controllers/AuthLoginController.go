package controllers

import (
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func AuthLogin(c echo.Context) error {
	var userValidation models.UserLoginValidation
	if status, err := helpers.Validate(c, &userValidation); err != nil {
		return c.String(status, err.Error())
	}

	// validate captcha
	if *config.ENV.CaptchaLoginEnabled {
		success, err := helpers.CaptchaValid(c)
		if err != nil {
			return c.String(http.StatusBadRequest, fmt.Sprint("Captcha error: ", err.Error()))
		}
		if !success {
			return c.String(http.StatusBadRequest, "Captcha incorrect")
		}
	}

	var user models.User
	res := inits.DB.Model(&models.User{}).Where(&models.User{
		Username: userValidation.Username,
	}).First(&user)
	if res.Error != nil {
		return c.String(http.StatusBadRequest, "User not found")
	}

	if !helpers.CheckPasswordHash(userValidation.Password, user.Hash) {
		return c.String(http.StatusBadRequest, "Wrong password")
	}

	tokenString, expirationTime, err := auth.GenerateJWT(user)
	if err != nil {
		log.Printf("Failed to generate jwt for user %s: %v\n", user.Username, err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, echo.Map{
		"exp":   expirationTime,
		"token": tokenString,
	})
}
