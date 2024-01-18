package controllers

import (
	"ch/kirari04/videocms/logic"

	"github.com/labstack/echo/v4"
)

func GetAccount(c echo.Context) error {
	status, dbAccount, err := logic.GetAccount(c.Get("UserID").(uint))
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(status, dbAccount)
}
