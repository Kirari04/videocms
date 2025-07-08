package controllers

import (
	"ch/kirari04/videocms/config"
	"fmt"
	"net/http"

	"github.com/imroc/req/v3"
	"github.com/labstack/echo/v4"
)

func GetVersionCheck(c echo.Context) error {

	client := req.C()
	res, err := client.R().
		SetContext(c.Request().Context()).
		Get("https://raw.githubusercontent.com/Kirari04/videocms/refs/heads/master/VERSION.txt")
	if err != nil {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("Error fetching version: %v", err.Error()))
	}
	if !res.IsSuccessState() {
		return c.String(http.StatusInternalServerError, fmt.Sprintf("Error fetching version with status %d: %v", res.StatusCode, res.String()))
	}

	newestVersion := res.String()

	if config.VERSION != newestVersion {
		return c.String(http.StatusBadRequest, fmt.Sprintf("A new version is available: %s. Current version: %s", newestVersion, config.VERSION))
	}

	return c.String(http.StatusOK, fmt.Sprintf("You are using the latest version: %s", config.VERSION))
}
