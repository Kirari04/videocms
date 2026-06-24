package controllers

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) ViewExampleUpload(c echo.Context) error {
	return c.Render(http.StatusOK, "examples/upload.html", echo.Map{
		"AppName": h.Config().AppName,
	})
}
