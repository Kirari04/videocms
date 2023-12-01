package controllers

import (
	"ch/kirari04/videocms/logic"

	"github.com/gofiber/fiber/v2"
)

func GetFileExample(c *fiber.Ctx) error {
	status, response, err := logic.GetFileExample()
	if err != nil {
		return c.Status(status).SendString(err.Error())
	}
	return c.Status(status).SendString(response)
}
