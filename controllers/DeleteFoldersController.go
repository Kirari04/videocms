package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/models"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) DeleteFolders(c echo.Context) error {
	// parse & validate request
	var folderValidation models.FoldersDeleteValidation
	if status, err := helpers.Validate(c, &folderValidation); err != nil {
		return c.String(status, err.Error())
	}

	ids := make([]uint, 0, len(folderValidation.FolderIDs))
	for _, item := range folderValidation.FolderIDs {
		ids = append(ids, item.FolderID)
	}
	return h.enqueueDeletion(c, nil, ids)
}
