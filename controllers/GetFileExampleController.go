package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func GetFileExample(c *fiber.Ctx) error {
	var link models.Link
	if res := inits.DB.First(&link); res.Error != nil {
		return c.Status(fiber.StatusNotFound).SendString(fiber.ErrNotFound.Message)
	}
	return c.SendString(fmt.Sprintf("%v", link.UUID))
}
