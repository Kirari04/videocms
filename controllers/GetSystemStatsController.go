package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"time"

	"github.com/gofiber/fiber/v2"
)

func GetSystemStats(c *fiber.Ctx) error {

	var resources []models.SystemResource
	if res := inits.DB.
		Where("created_at > ?", time.Now().Add(time.Hour*24*-1)).
		Where("server_id IS NULL").
		Find(&resources); res.Error != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.JSON(&resources)
}
