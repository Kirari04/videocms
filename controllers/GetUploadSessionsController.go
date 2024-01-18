package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

type GetUploadSessionsRes struct {
	CreatedAt   *time.Time
	Name        string
	UUID        string
	Size        int64
	ChunckCount int
}

func GetUploadSessions(c echo.Context) error {
	userId, ok := c.Get("UserID").(uint)
	if !ok {
		log.Println("GetUploadSessions: Failed to catch userId")
		return c.NoContent(http.StatusInternalServerError)
	}

	var sessions []GetUploadSessionsRes
	if res := inits.DB.
		Model(&models.UploadSession{}).
		Where(&models.UploadSession{
			UserID: userId,
		}, "UserID").
		Find(&sessions); res.Error != nil {
		log.Println("Failed to list upload sessions", res.Error)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, &sessions)
}
