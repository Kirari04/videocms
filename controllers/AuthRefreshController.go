package controllers

import (
	"ch/kirari04/videocms/auth"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func AuthRefresh(c *fiber.Ctx) error {
	bearer := c.GetReqHeaders()["Authorization"]
	if len(bearer) == 0 || bearer[0] == "" {
		return c.SendStatus(fiber.StatusForbidden)
	}
	bearerHeader := strings.Split(bearer[0], " ")
	tokenString := bearerHeader[len(bearerHeader)-1]
	newTokenString, expirationTime, err := auth.RefreshJWT(tokenString)
	if err != nil {
		return c.Status(fiber.StatusForbidden).SendString(err.Error())
	}

	return c.JSON(fiber.Map{
		"exp":   expirationTime,
		"token": newTokenString,
	})
}
