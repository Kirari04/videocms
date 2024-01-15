package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"

	"github.com/gofiber/fiber/v2"
)

func GetSettings(c *fiber.Ctx) error {
	_, ok := c.Locals("UserID").(uint)
	if !ok {
		log.Println("Failed to catch user")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	var setting models.Setting
	if res := inits.DB.FirstOrCreate(&setting); res.Error != nil {
		log.Fatalln("Failed to get settings", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	return c.Status(fiber.StatusOK).JSON(&setting)
}
