package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/models"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) MoveItems(c echo.Context) error {
	// parse & validate request
	var moveValidation models.MoveItemsValidation
	if status, err := helpers.Validate(c, &moveValidation); err != nil {
		return c.String(status, err.Error())
	}

	// Determine admin status
	isAdmin, _ := c.Get("Admin").(bool)

	status, err := h.Logic.MoveItems(
		c.Get("UserID").(uint),
		moveValidation.ParentFolderID,
		moveValidation.FolderIDs,
		moveValidation.LinkIDs,
		isAdmin,
	)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.NoContent(status)
}
