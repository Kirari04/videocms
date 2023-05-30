package controllers

import (
	"ch/kirari04/videocms/config"

	"github.com/gofiber/fiber/v2"
)

func GetConfig(c *fiber.Ctx) error {
	return c.JSON(config.ENV.PublicConfig())
}
