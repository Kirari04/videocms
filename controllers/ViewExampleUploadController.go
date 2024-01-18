package controllers

import (
	"ch/kirari04/videocms/config"
	"net/http"

	"github.com/labstack/echo/v4"
)

func ViewExampleUpload(c echo.Context) error {
	return c.Render(http.StatusOK, "examples/upload", echo.Map{
		"AppName": config.ENV.AppName,
	})
}
