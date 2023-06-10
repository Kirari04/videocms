package controllers

import (
	"github.com/gofiber/fiber/v2"
)

func ViewExampleUpload(c *fiber.Ctx) error {
	return c.Render("examples/upload", fiber.Map{
		"ExampleVideo": "no",
	})
}
