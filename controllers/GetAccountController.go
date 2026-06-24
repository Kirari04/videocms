package controllers

import (
	"github.com/labstack/echo/v4"
)

func (h *Handlers) GetAccount(c echo.Context) error {
	status, dbAccount, err := h.Logic.GetAccount(c.Get("UserID").(uint))
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(status, dbAccount)
}
