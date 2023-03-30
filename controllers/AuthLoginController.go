package controllers

import (
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"
)

func AuthLogin(c *fiber.Ctx) error {
	var userValidation models.UserLoginValidation
	if err := c.BodyParser(&userValidation); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}

	if errors := helpers.ValidateStruct(userValidation); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

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
