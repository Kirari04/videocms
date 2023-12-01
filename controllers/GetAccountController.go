package controllers

import (
	"ch/kirari04/videocms/logic"

	"github.com/gofiber/fiber/v2"
)

func GetAccount(c *fiber.Ctx) error {
	status, dbAccount, err := logic.GetAccount(c.Locals("UserID").(uint))
	if err != nil {
		return c.Status(status).SendString(err.Error())
	}

	return c.Status(status).JSON(dbAccount)
}
