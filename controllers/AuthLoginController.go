package controllers

import (
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"

	"github.com/gofiber/fiber/v2"
)

func AuthLogin(c *fiber.Ctx) error {
	var userValidation models.UserLoginValidation
	if status, err := helpers.Validate(c, &userValidation, "body"); err != nil {
		return c.Status(status).SendString(err.Error())
	}

	// validate captcha
	// success, err := helpers.CaptchaValid(c)

	var user models.User
	res := inits.DB.Model(&models.User{}).Where(&models.User{
		Username: userValidation.Username,
	}).First(&user)
	if res.Error != nil {
		return c.Status(fiber.StatusNotFound).SendString("User not found")
	}

	if !helpers.CheckPasswordHash(userValidation.Password, user.Hash) {
		return c.Status(fiber.StatusBadRequest).SendString("Wrong password")
	}

	tokenString, expirationTime, err := auth.GenerateJWT(user)
	if err != nil {
		log.Printf("Failed to generate jwt for user %s: %v\n", user.Username, err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.JSON(fiber.Map{
		"exp":   expirationTime,
		"token": tokenString,
	})
}
