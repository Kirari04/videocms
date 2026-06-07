package controllers

import (
	"ch/kirari04/videocms/services/tusupload"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func GetUploadSessions(c echo.Context) error {
	userID, ok := c.Get("UserID").(uint)
	if !ok {
		log.Println("GetUploadSessions: Failed to catch userId")
		return c.NoContent(http.StatusInternalServerError)
	}

	sessions, err := tusupload.ListSessions(userID)
	if err != nil {
		log.Println("Failed to list upload sessions", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, &sessions)
}
