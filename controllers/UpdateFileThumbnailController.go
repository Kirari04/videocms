package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/models"
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) UpdateFileThumbnail(c echo.Context) error {
	var validation models.LinkThumbnailValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}

	file, err := c.FormFile("thumbnail")
	if err != nil {
		return c.String(http.StatusBadRequest, "No thumbnail uploaded")
	}
	src, err := file.Open()
	if err != nil {
		c.Logger().Error("Failed to open thumbnail file", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	defer src.Close()

	isAdmin, _ := c.Get("Admin").(bool)
	status, err := h.Logic.UpdateLinkThumbnail(
		validation.LinkID,
		c.Get("UserID").(uint),
		isAdmin,
		src,
		file.Size,
		file.Header.Get("Content-Type"),
	)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.NoContent(status)
}

func (h *Handlers) DeleteFileThumbnail(c echo.Context) error {
	var validation models.LinkThumbnailValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}

	isAdmin, _ := c.Get("Admin").(bool)
	status, err := h.Logic.ResetLinkThumbnail(validation.LinkID, c.Get("UserID").(uint), isAdmin)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.NoContent(status)
}
