package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"

	"github.com/gofiber/fiber/v2"
)

type listPublicWebPageRes struct {
	Path         string
	Title        string
	ListInFooter bool
}

func ListPublicWebPage(c *fiber.Ctx) error {
	var webPages []listPublicWebPageRes
	if res := inits.DB.
		Model(&models.WebPage{}).
		Select(
			"path",
			"title",
			"list_in_footer",
		).
		Find(&webPages); res.Error != nil {
		log.Println("Failed to list webpages", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.Status(fiber.StatusOK).JSON(&webPages)
}
