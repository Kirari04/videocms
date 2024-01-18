package controllers

import (
	"ch/kirari04/videocms/auth"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/labstack/echo/v4"
)

func AuthRefresh(c echo.Context) error {
	bearer := c.Request().Header.Get("Authorization")
	if bearer == "" {
		return c.NoContent(fiber.StatusForbidden)
	}
	bearerHeader := strings.Split(bearer, " ")
	tokenString := bearerHeader[len(bearerHeader)-1]
	newTokenString, expirationTime, err := auth.RefreshJWT(tokenString)
	if err != nil {
		return c.String(fiber.StatusForbidden, err.Error())
	}

	return c.JSON(http.StatusOK, echo.Map{
		"exp":   expirationTime,
		"token": newTokenString,
	})
}
