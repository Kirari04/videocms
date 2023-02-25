package controllers

import (
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"

	"github.com/gofiber/fiber/v2"
)

func Login(c *fiber.Ctx) error {
	var userValidation models.UserLoginValidation
	if err := c.BodyParser(&userValidation); err != nil {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "none",
				Tag:         "none",
				Value:       "Invalid body request format",
			},
		})
	}

	if errors := helpers.ValidateStruct(userValidation); len(errors) > 0 {
		return c.Status(400).JSON(errors)
	}

	var user models.User
	res := inits.DB.Model(&models.User{}).Where(&models.User{
		Username: userValidation.Username,
	}).First(&user)
	if res.Error != nil {
		return c.Status(404).JSON([]helpers.ValidationError{
			{
				FailedField: "username",
				Tag:         "none",
				Value:       "User not found",
			},
		})
	}

	if !helpers.CheckPasswordHash(userValidation.Password, user.Hash) {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "password",
				Tag:         "none",
				Value:       "Wrong password",
			},
		})
	}

	tokenString, err := auth.GenerateJWT(user)
	if err != nil {
		log.Printf("Failed to generate jwt for user %s\n", user.Username)
		log.Println(err.Error())
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.SendString(tokenString)
}
