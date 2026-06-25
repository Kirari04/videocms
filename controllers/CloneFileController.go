package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/models"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) CloneFile(c echo.Context) error {
	// parse & validate request
	var fileValidation models.FileCloneValidation
	if status, err := helpers.Validate(c, &fileValidation); err != nil {
		return c.String(status, err.Error())
	}

	// business logic
	status, dbLink, err := h.Logic.CloneFileByHash(fileValidation.Sha256, fileValidation.ParentFolderID, fileValidation.Name, c.Get("UserID").(uint), "")
	if err != nil {
		return c.String(status, err.Error())
	}

	return c.JSON(status, dbLink)
}
