package controllers

import (
	"ch/kirari04/videocms/models"
	"net/http"

	"github.com/labstack/echo/v4"
)

type listPublicWebPageRes struct {
	Path         string
	Title        string
	ListInFooter bool
}

func (h *Handlers) ListPublicWebPage(c echo.Context) error {
	var webPages []listPublicWebPageRes
	if res := h.Deps.DB.
		Model(&models.WebPage{}).
		Select(
			"path",
			"title",
			"list_in_footer",
		).
		Find(&webPages); res.Error != nil {
		c.Logger().Error("Failed to list webpages", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, &webPages)
}
