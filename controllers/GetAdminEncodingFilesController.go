package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"net/http"

	"github.com/labstack/echo/v4"
)

type GetAdminEncodingFilesRes struct {
	ID       uint    `json:"id"`
	Name     string  `json:"name"`
	Quality  string  `json:"quality"`
	Progress float64 `json:"progress"`
	UserID   uint    `json:"user_id"`
	Username string  `json:"username"`
}

func GetAdminEncodingFiles(c echo.Context) error {
	var res []GetAdminEncodingFilesRes

	// Qualities
	var resQuality []GetAdminEncodingFilesRes
	if err := inits.DB.
		Model(&models.Link{}).
		Select(
			"links.id as id",
			"links.name as name",
			"qualities.name as quality",
			"qualities.progress as progress",
			"users.id as user_id",
			"users.username as username",
		).
		Joins("JOIN users ON users.id = links.user_id").
		Joins("JOIN files ON files.id = links.file_id").
		Joins("JOIN qualities ON files.id = qualities.file_id AND qualities.failed = ? AND qualities.ready = ?", false, false).
		Order("files.id ASC").
		Scan(&resQuality).Error; err != nil {
		c.Logger().Error("Failed to list admin encoding files (qualities)", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	// Audios
	var resAudio []GetAdminEncodingFilesRes
	if err := inits.DB.
		Model(&models.Link{}).
		Select(
			"links.id as id",
			"links.name as name",
			"audios.name as quality",
			"audios.progress as progress",
			"users.id as user_id",
			"users.username as username",
		).
		Joins("JOIN users ON users.id = links.user_id").
		Joins("JOIN files ON files.id = links.file_id").
		Joins("JOIN audios ON files.id = audios.file_id AND audios.failed = ? AND audios.ready = ?", false, false).
		Order("files.id ASC").
		Scan(&resAudio).Error; err != nil {
		c.Logger().Error("Failed to list admin encoding files (audios)", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	// Subtitles
	var resSubtitle []GetAdminEncodingFilesRes
	if err := inits.DB.
		Model(&models.Link{}).
		Select(
			"links.id as id",
			"links.name as name",
			"subtitles.name as quality",
			"subtitles.progress as progress",
			"users.id as user_id",
			"users.username as username",
		).
		Joins("JOIN users ON users.id = links.user_id").
		Joins("JOIN files ON files.id = links.file_id").
		Joins("JOIN subtitles ON files.id = subtitles.file_id AND subtitles.failed = ? AND subtitles.ready = ?", false, false).
		Order("files.id ASC").
		Scan(&resSubtitle).Error; err != nil {
		c.Logger().Error("Failed to list admin encoding files (subtitles)", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	res = append(res, resSubtitle...)
	res = append(res, resAudio...)
	res = append(res, resQuality...)

	return c.JSON(http.StatusOK, &res)
}
