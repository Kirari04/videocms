package controllers

import (
	"ch/kirari04/videocms/auth"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func AuthRefresh(c *fiber.Ctx) error {
	bearer := c.GetReqHeaders()["Authorization"]
	if bearer == "" {
		return c.SendStatus(fiber.StatusForbidden)
	}
	bearerHeader := strings.Split(bearer, " ")
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
