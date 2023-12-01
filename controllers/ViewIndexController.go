package controllers

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func ViewIndex(c *fiber.Ctx) error {
	var link models.Link
	if res := inits.DB.First(&link); res.Error != nil {
		return c.Render("index", fiber.Map{
			"ExampleVideo":         fmt.Sprintf("/%v", "notfound"),
			"AppName":              config.ENV.AppName,
			"ProjectDocumentation": config.ENV.ProjectDocumentation,
			"ProjectDownload":      config.ENV.ProjectDownload,
		})
	}
	return c.Render("index", fiber.Map{
		"ExampleVideo":         fmt.Sprintf("/%v", link.UUID),
		"AppName":              config.ENV.AppName,
		"ProjectDocumentation": config.ENV.ProjectDocumentation,
		"ProjectDownload":      config.ENV.ProjectDownload,
	})
}
