package controllers

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) AuthLogout(c echo.Context) error {
	parts := strings.Fields(c.Request().Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.HasPrefix(parts[1], "ak_") {
		return c.String(http.StatusForbidden, "A session token is required")
	}
	if err := h.Auth.RevokeJWT(parts[1]); err != nil {
		return c.String(http.StatusForbidden, "Invalid session token")
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.NoContent(http.StatusNoContent)
}
