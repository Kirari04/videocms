package middlewares

import (
	"ch/kirari04/videocms/auth"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func Auth(c *fiber.Ctx) error {
	bearer := c.GetReqHeaders()["Authorization"]
	if bearer == "" {
		return c.SendStatus(fiber.StatusForbidden)
	}
	bearerHeader := strings.Split(bearer, " ")
	tokenString := bearerHeader[len(bearerHeader)-1]
	token, claims, err := auth.VerifyJWT(tokenString)
	if err != nil {
		return c.SendStatus(fiber.StatusForbidden)
	}
	if !token.Valid {
		return c.SendStatus(fiber.StatusForbidden)
	}
	c.Locals("Username", claims.Username)
	c.Locals("UserID", claims.UserID)
	c.Locals("Admin", claims.Admin)
	return c.Next()
}
