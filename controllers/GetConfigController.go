package controllers

import (
	"ch/kirari04/videocms/config"
	"net/http"

	"github.com/labstack/echo/v4"
)

func GetConfig(c echo.Context) error {
	return c.JSON(http.StatusOK, config.ENV.PublicConfig())
}
