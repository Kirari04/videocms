package controllers

import (
	"ch/kirari04/videocms/logic"
	"github.com/labstack/echo/v4"
)

func GetUsers(c echo.Context) error {
	status, users, err := logic.GetUsers()
	if err != nil {
		return c.String(status, err.Error())
	}
	return c.JSON(status, users)
}