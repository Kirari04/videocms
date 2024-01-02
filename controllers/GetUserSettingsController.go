package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"

	"github.com/gofiber/fiber/v2"
)

func GetUserSettingsController(c *fiber.Ctx) error {
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

	return c.JSON(fiber.Map{
		"EnablePlayerCaptcha": user.Settings.EnablePlayerCaptcha,
	})
}
