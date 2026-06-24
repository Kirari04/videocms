package controllers

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) GetConfig(c echo.Context) error {
	return c.JSON(http.StatusOK, h.Config().PublicConfig())
}
