package middlewares

import (
	"ch/kirari04/videocms/auth"

	"github.com/gofiber/fiber/v2"
)

func JwtStream(c *fiber.Ctx) error {
	uuid := c.Params("UUID", "")
	if uuid == "" {
		return c.Status(fiber.StatusBadRequest).SendString("Missing UUID parameter")
	}
	tknStr := c.Cookies(uuid, "")
	if uuid == "" {
		return c.Status(fiber.StatusBadRequest).SendString("UUID parameter match issue")
	}
	token, claims, err := auth.VerifyJWTStream(tknStr)
	if err != nil {
		return c.Status(fiber.StatusForbidden).SendString("Broken JWT")
	}
	if !token.Valid {
		return c.Status(fiber.StatusForbidden).SendString("Invalid JWT")
	}
	if claims.UUID != uuid {
		return c.Status(fiber.StatusForbidden).SendString("Mismacht UUID")
	}
	return c.Next()
}
