package middlewares

import (
	"github.com/gofiber/fiber/v2"
)

func IsAdmin(c *fiber.Ctx) error {
	isAdmin, ok := c.Locals("Admin").(bool)
	if !ok {
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	if !isAdmin {
		return c.Status(fiber.StatusForbidden).SendString("Not Permitted")
	}
	return c.Next()
}
