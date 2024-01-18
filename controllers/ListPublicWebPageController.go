package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/labstack/echo/v4"
)

type listPublicWebPageRes struct {
	Path         string
	Title        string
	ListInFooter bool
}

func ListPublicWebPage(c echo.Context) error {
	var webPages []listPublicWebPageRes
	if res := inits.DB.
		Model(&models.WebPage{}).
		Select(
			"path",
			"title",
			"list_in_footer",
		).
		Find(&webPages); res.Error != nil {
		c.Logger().Error("Failed to list webpages", res.Error)
		return c.NoContent(fiber.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, &webPages)
}
