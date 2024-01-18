package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"net/http"

	"github.com/labstack/echo/v4"
)

type GetEncodingFilesRes struct {
	ID       uint
	Name     string
	Quality  string
	Progress float64
}

func GetEncodingFiles(c echo.Context) error {
	userId, ok := c.Get("UserID").(uint)
	if !ok {
		c.Logger().Error("Failed to catch user")
		return c.NoContent(http.StatusInternalServerError)
	}
	var res []GetEncodingFilesRes
	if err := inits.DB.
		Model(&models.Link{}).
		Select(
			"links.id as id",
			"links.name as name",
			"qualities.name as quality",
			"qualities.progress as progress",
		).
		Where("links.user_id = ?", userId).
		Joins("JOIN files ON files.id = links.file_id").
		Joins("JOIN qualities ON files.id = qualities.file_id AND qualities.encoding = ? AND qualities.failed = ? AND qualities.ready = ?", true, false, false).
		Scan(&res).Error; err != nil {
		c.Logger().Error("Failed to list encoding files", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, &res)
}
