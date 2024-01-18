package controllers

import (
	"ch/kirari04/videocms/logic"

	"github.com/labstack/echo/v4"
)

func GetFileExample(c echo.Context) error {
	status, response, err := logic.GetFileExample()
	if err != nil {
		return c.String(status, err.Error())
	}
	return c.String(status, response)
}
