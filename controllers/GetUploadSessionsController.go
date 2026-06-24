package controllers

import (
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) GetUploadSessions(c echo.Context) error {
	userID, ok := c.Get("UserID").(uint)
	if !ok {
		log.Println("GetUploadSessions: Failed to catch userId")
		return c.NoContent(http.StatusInternalServerError)
	}

	if h == nil || h.TUS == nil {
		log.Println("GetUploadSessions: upload service is nil")
		return c.NoContent(http.StatusInternalServerError)
	}

	sessions, err := h.TUS.ListSessions(userID)
	if err != nil {
		log.Println("Failed to list upload sessions", err)
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.JSON(http.StatusOK, &sessions)
}
