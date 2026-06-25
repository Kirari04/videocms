package controllers

import (
	"ch/kirari04/videocms/models"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) ViewIndex(c echo.Context) error {
	var link models.Link
	if res := h.Deps.DB.First(&link); res.Error != nil {
		return c.Render(http.StatusOK, "index.html", echo.Map{
			"ExampleVideo": fmt.Sprintf("/%v", "notfound"),
			"AppName":      h.Config().AppName,
		})
	}
	return c.Render(http.StatusOK, "index.html", echo.Map{
		"ExampleVideo": fmt.Sprintf("/%v", link.UUID),
		"AppName":      h.Config().AppName,
	})
}
