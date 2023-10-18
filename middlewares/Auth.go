package middlewares

import (
	"ch/kirari04/videocms/auth"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func Auth(c *fiber.Ctx) error {
	bearer := c.GetReqHeaders()["Authorization"]
	if len(bearer) == 0 || bearer[0] == "" {
		return c.Status(fiber.StatusForbidden).SendString("No JWT Token")
	}
	bearerHeader := strings.Split(bearer[0], " ")
	tokenString := bearerHeader[len(bearerHeader)-1]
	token, claims, err := auth.VerifyJWT(tokenString)
	if err != nil {
		return c.Status(fiber.StatusForbidden).SendString("Invalid JWT Token")
	}
	if !token.Valid {
		return c.Status(fiber.StatusForbidden).SendString("Expired JWT Token")
	}
	c.Locals("Username", claims.Username)
	c.Locals("UserID", claims.UserID)
	c.Locals("Admin", claims.Admin)
	return c.Next()
}
