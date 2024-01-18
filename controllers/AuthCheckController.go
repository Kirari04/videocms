package controllers

import (
	"ch/kirari04/videocms/auth"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

func AuthCheck(c echo.Context) error {
	bearer := c.Request().Header.Get("Authorization")
	if bearer == "" {
		return c.NoContent(http.StatusForbidden)
	}
	bearerHeader := strings.Split(bearer, " ")
	tokenString := bearerHeader[len(bearerHeader)-1]
	token, claims, err := auth.VerifyJWT(tokenString)
	if err != nil {
		return c.NoContent(http.StatusForbidden)
	}
	if !token.Valid {
		return c.NoContent(http.StatusForbidden)
	}
	return c.JSON(http.StatusOK, echo.Map{
		"username": claims.Username,
		"exp":      claims.ExpiresAt.Time,
	})
}
