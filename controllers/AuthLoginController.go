package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

// Same bcrypt cost as production password hashes, generated once for the
// missing-account path so a failed lookup still performs password work.
const dummyLoginHash = "$2b$14$DV.O1wq5WjyMPWD7z4yFhe.D.QGRYiJPK7/.MGo6dH2QCCJWsCb9C"

func (h *Handlers) AuthLogin(c echo.Context) error {
	var userValidation models.UserLoginValidation
	if status, err := helpers.Validate(c, &userValidation); err != nil {
		return c.String(status, err.Error())
	}

	// validate captcha
	if *h.Config().CaptchaLoginEnabled {
		success, err := h.Auth.CaptchaValid(c)
		if err != nil {
			return c.String(http.StatusBadRequest, fmt.Sprint("Captcha error: ", err.Error()))
		}
		if !success {
			return c.String(http.StatusBadRequest, "Captcha incorrect")
		}
	}

	var user models.User
	res := h.Deps.DB.Model(&models.User{}).Where(&models.User{
		Username: userValidation.Username,
	}).First(&user)
	if res.Error != nil && !errors.Is(res.Error, gorm.ErrRecordNotFound) {
		log.Println("Failed to query login account", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}
	hash := user.Hash
	if errors.Is(res.Error, gorm.ErrRecordNotFound) {
		hash = dummyLoginHash
	}
	passwordMatches := helpers.CheckPasswordHash(userValidation.Password, hash)
	if res.Error != nil || !passwordMatches {
		return c.String(http.StatusBadRequest, "Invalid credentials")
	}

	tokenString, expirationTime, err := h.Auth.GenerateJWT(user)
	if err != nil {
		log.Printf("Failed to generate jwt for user %s: %v\n", user.Username, err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, echo.Map{
		"exp":   expirationTime,
		"token": tokenString,
	})
}
