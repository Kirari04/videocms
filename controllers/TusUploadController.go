package controllers

import (
	"ch/kirari04/videocms/services/tusupload"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

func TusUpload(c echo.Context) error {
	return tusupload.EchoHandler(c)
}

func FinalizeTusUpload(c echo.Context) error {
	userID, ok := c.Get("UserID").(uint)
	if !ok {
		log.Println("FinalizeTusUpload: Failed to catch userId")
		return c.NoContent(http.StatusInternalServerError)
	}

	status, response, err := tusupload.Finalize(c.Param("upload_id"), userID)
	if err != nil {
		return c.String(status, err.Error())
	}
	return c.JSON(status, response)
}
