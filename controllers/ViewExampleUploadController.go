package controllers

import (
	"ch/kirari04/videocms/config"

	"github.com/gofiber/fiber/v2"
)

func ViewExampleUpload(c *fiber.Ctx) error {
	return c.Render("examples/upload", fiber.Map{
		"AppName": config.ENV.AppName,
	})
}
