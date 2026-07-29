package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handlers) GetWebPage(c echo.Context) error {
	webPageID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || webPageID == 0 {
		return c.String(http.StatusBadRequest, "Invalid webpage ID")
	}

	var webPage models.WebPage
	if res := h.Deps.DB.First(&webPage, uint(webPageID)); res.Error != nil {
		if errors.Is(res.Error, gorm.ErrRecordNotFound) {
			return c.String(http.StatusNotFound, "Webpage not found")
		}
		c.Logger().Error("Failed to find webpage", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, &webPage)
}

func (h *Handlers) PreviewWebPage(c echo.Context) error {
	var validatus models.WebPagePreviewValidation
	if status, err := helpers.Validate(c, &validatus); err != nil {
		return c.String(status, err.Error())
	}

	rendered, err := RenderWebPageContent(validatus.Format, validatus.Content)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}
	return c.String(http.StatusOK, rendered)
}

func normalizeWebPagePath(input string) (string, error) {
	path := strings.Trim(strings.TrimSpace(input), "/")
	if path == "" {
		return "", errors.New("Path must contain at least one segment")
	}

	normalized := fmt.Sprintf("/%s/", path)
	if len(normalized) > 50 {
		return "", errors.New("Path must be 50 characters or fewer")
	}
	return normalized, nil
}
