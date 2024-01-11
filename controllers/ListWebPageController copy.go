package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"

	"github.com/gofiber/fiber/v2"
)

func ListWebPage(c *fiber.Ctx) error {
	var webPages []models.WebPage
	if res := inits.DB.Find(&webPages); res.Error != nil {
		log.Println("Failed to list webpages", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	return c.Status(fiber.StatusOK).JSON(&webPages)
}
