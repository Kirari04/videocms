package controllers

import (
	"ch/kirari04/videocms/auth"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func AuthCheck(c *fiber.Ctx) error {
	bearer := c.GetReqHeaders()["Authorization"]
	if bearer == "" {
		return c.SendStatus(fiber.StatusForbidden)
	}
	bearerHeader := strings.Split(bearer, " ")
	tokenString := bearerHeader[len(bearerHeader)-1]
	token, claims, err := auth.VerifyJWT(tokenString)
	if err != nil {
		return c.Status(fiber.StatusForbidden).SendString(err.Error())
	}
	if !token.Valid {
		return c.Status(fiber.StatusForbidden).SendString(token.Raw)
	}
	return c.JSON(fiber.Map{
		"username": claims.Username,
		"exp":      claims.ExpiresAt.Time,
	})
}
