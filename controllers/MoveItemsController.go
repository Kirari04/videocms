package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"

	"github.com/labstack/echo/v4"
)

func MoveItems(c echo.Context) error {
	// parse & validate request
	var moveValidation models.MoveItemsValidation
	if status, err := helpers.Validate(c, &moveValidation); err != nil {
		return c.String(status, err.Error())
	}

	status, err := logic.MoveItems(
		c.Get("UserID").(uint),
		moveValidation.ParentFolderID,
		moveValidation.FolderIDs,
		moveValidation.LinkIDs,
	)
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.NoContent(status)
}
