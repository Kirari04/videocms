package controllers

import (
	"github.com/labstack/echo/v4"
)

func (h *Handlers) GetFileExample(c echo.Context) error {
	status, response, err := h.Logic.GetFileExample()
	if err != nil {
		return c.String(status, err.Error())
	}
	return c.String(status, response)
}
