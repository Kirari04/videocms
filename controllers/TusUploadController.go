package controllers

import (
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) TusUpload(c echo.Context) error {
	if h == nil || h.TUS == nil {
		log.Println("TusUpload: upload service is nil")
		return c.NoContent(http.StatusInternalServerError)
	}
	return h.TUS.EchoHandler(c)
}

func (h *Handlers) FinalizeTusUpload(c echo.Context) error {
	userID, ok := c.Get("UserID").(uint)
	if !ok {
		log.Println("FinalizeTusUpload: Failed to catch userId")
		return c.NoContent(http.StatusInternalServerError)
	}

	if h == nil || h.TUS == nil {
		log.Println("FinalizeTusUpload: upload service is nil")
		return c.NoContent(http.StatusInternalServerError)
	}

	status, response, err := h.TUS.Finalize(c.Param("upload_id"), userID)
	if err != nil {
		return c.String(status, err.Error())
	}
	return c.JSON(status, response)
}
