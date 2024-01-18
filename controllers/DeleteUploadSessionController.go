package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
)

func DeleteUploadSession(c echo.Context) error {
	// parse & validate request
	var validation models.DeleteUploadSessionValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}

	userId, ok := c.Get("UserID").(uint)
	if !ok {
		log.Println("GetUploadSessions: Failed to catch userId")
		return c.NoContent(http.StatusInternalServerError)
	}

	var uploadSession models.UploadSession
	if res := inits.DB.Where(&models.UploadSession{
		UUID: validation.UploadSessionUUID,
	}, "UUID").First(&uploadSession); res.Error != nil {
		return c.String(http.StatusBadRequest, "Upload Session not found")
	}

	if uploadSession.UserID != userId {
		return c.String(http.StatusBadRequest, "Upload Session not found")
	}

	if res := inits.DB.
		Model(&models.UploadChunck{}).
		Where(&models.UploadChunck{
			UploadSessionID: uploadSession.ID,
		}).
		Delete(&models.UploadChunck{}); res.Error != nil {
		log.Printf("[WARNING] createUploadFileCleanup -> remove upload chuncks from database (%d): %v\n", uploadSession.ID, res.Error)
	}
	if res := inits.DB.
		Delete(&models.UploadSession{}, uploadSession.ID); res.Error != nil {
		log.Printf("[WARNING] createUploadFileCleanup -> remove upload session from database (%d): %v\n", uploadSession.ID, res.Error)
	}

	if err := os.RemoveAll(uploadSession.SessionFolder); err != nil {
		log.Printf("[WARNING] createUploadFileCleanup -> remove session folder: %v\n", err)
	}

	return c.String(http.StatusOK, "ok")
}
