package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"

	"github.com/gofiber/fiber/v2"
)

func ListServers(c *fiber.Ctx) error {
	var servers []models.Server
	if res := inits.DB.Order("hostname ASC").Find(&servers); res.Error != nil {
		log.Println("Failed to list servers", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.Status(fiber.StatusOK).JSON(&servers)
}
