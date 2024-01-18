package controllers

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
)

func ViewIndex(c echo.Context) error {
	var link models.Link
	if res := inits.DB.First(&link); res.Error != nil {
		return c.Render(http.StatusOK, "index.html", echo.Map{
			"ExampleVideo":         fmt.Sprintf("/%v", "notfound"),
			"AppName":              config.ENV.AppName,
			"ProjectDocumentation": config.ENV.ProjectDocumentation,
			"ProjectDownload":      config.ENV.ProjectDownload,
		})
	}
	return c.Render(http.StatusOK, "index.html", echo.Map{
		"ExampleVideo":         fmt.Sprintf("/%v", link.UUID),
		"AppName":              config.ENV.AppName,
		"ProjectDocumentation": config.ENV.ProjectDocumentation,
		"ProjectDownload":      config.ENV.ProjectDownload,
	})
}
