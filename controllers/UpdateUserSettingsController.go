package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"

	"github.com/gofiber/fiber/v2"
)

func UpdateUserSettingsController(c *fiber.Ctx) error {
	// parse & validate request
	var validater models.UserSettingsUpdateValidation
	if err := c.BodyParser(&validater); err != nil {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "none",
				Tag:         "none",
				Value:       "Invalid body request format",
			},
		})
	}
	if errors := helpers.ValidateStruct(validater); len(errors) > 0 {
		return c.Status(400).JSON(errors)
	}

	userId, ok := c.Locals("UserID").(uint)
	if !ok {
		log.Println("Failed to catch userID")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	var user models.User
	if res := inits.DB.First(&user, userId); res.Error != nil {
		log.Println("Failed to catch userID on db")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	user.Settings.EnablePlayerCaptcha = *validater.EnablePlayerCaptcha

	if res := inits.DB.Save(&user); res.Error != nil {
		log.Println("Failed to update user settings", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.SendStatus(fiber.StatusOK)
}
