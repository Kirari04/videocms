package controllers

import (
	"ch/kirari04/videocms/auth"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

func AuthRefresh(c echo.Context) error {
	bearer := c.Request().Header.Get("Authorization")
	if bearer == "" {
		return c.NoContent(http.StatusForbidden)
	}
	bearerHeader := strings.Split(bearer, " ")
	tokenString := bearerHeader[len(bearerHeader)-1]

	if strings.HasPrefix(tokenString, "ak_") {
		return c.String(http.StatusForbidden, "API Keys cannot be refreshed")
	}

	newTokenString, expirationTime, err := auth.RefreshJWT(tokenString)
	if err != nil {
		return c.String(http.StatusForbidden, err.Error())
	}

	return c.JSON(http.StatusOK, echo.Map{
		"exp":   expirationTime,
		"token": newTokenString,
	})
}
