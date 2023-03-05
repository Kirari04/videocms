package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func ViewIndex(c *fiber.Ctx) error {
	var link models.Link
	if res := inits.DB.First(&link); res.Error != nil {
		return c.Render("index", fiber.Map{
			"ExampleVideo": fmt.Sprintf("/%v", "notfound"),
		})
	}
	return c.Render("index", fiber.Map{
		"ExampleVideo": fmt.Sprintf("/%v", link.UUID),
	})
}
