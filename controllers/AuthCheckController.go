package controllers

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) AuthCheck(c echo.Context) error {
	bearer := c.Request().Header.Get("Authorization")
	if bearer == "" {
		return c.NoContent(http.StatusForbidden)
	}
	bearerHeader := strings.Split(bearer, " ")
	tokenString := bearerHeader[len(bearerHeader)-1]

	if strings.HasPrefix(tokenString, "ak_") {
		apiKey, err := h.Auth.VerifyApiKey(tokenString)
		if err != nil {
			return c.NoContent(http.StatusForbidden)
		}

		return c.JSON(http.StatusOK, echo.Map{
			"username":   apiKey.User.Username,
			"is_api_key": true,
			"exp":        apiKey.ExpiresAt,
		})
	}

	token, claims, err := h.Auth.VerifyJWT(tokenString)
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
