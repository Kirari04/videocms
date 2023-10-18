package controllers

import (
	"ch/kirari04/videocms/auth"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func AuthCheck(c *fiber.Ctx) error {
	bearer := c.GetReqHeaders()["Authorization"]
	if len(bearer) == 0 || bearer[0] == "" {
		return c.SendStatus(fiber.StatusForbidden)
	}
	bearerHeader := strings.Split(bearer[0], " ")
	tokenString := bearerHeader[len(bearerHeader)-1]
	token, claims, err := auth.VerifyJWT(tokenString)
	if err != nil {
		return c.SendStatus(fiber.StatusForbidden)
	}
	if !token.Valid {
		return c.SendStatus(fiber.StatusForbidden)
	}
	return c.JSON(fiber.Map{
		"username": claims.Username,
		"exp":      claims.ExpiresAt.Time,
	})
}
