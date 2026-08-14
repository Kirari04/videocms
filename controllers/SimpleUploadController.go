package controllers

import (
	"ch/kirari04/videocms/background"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handlers) SimpleUploadController(c echo.Context) error {
	// check if uploads are enabled
	if !*h.Config().UploadEnabled {
		return c.String(http.StatusForbidden, "Uploads are disabled")
	}

	userID, ok := c.Get("UserID").(uint)
	if !ok {
		return c.String(http.StatusInternalServerError, "Failed to catch UserID")
	}

	// parse & validate request
	var validation models.SimpleUploadValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}

	// file processing
	file, err := c.FormFile("file")
	if err != nil {
		return c.String(http.StatusBadRequest, "No file uploaded")
	}

	// size check
	if file.Size > h.Config().MaxUploadFilesize {
		return c.String(http.StatusRequestEntityTooLarge, fmt.Sprintf("Exceeded max upload filesize: %v", h.Config().MaxUploadFilesize))
	}

	requestKey := strings.TrimSpace(c.Request().Header.Get("Idempotency-Key"))
	uploadID := ""
	if requestKey != "" {
		uploadID = uuid.NewSHA1(uuid.NameSpaceURL, []byte(fmt.Sprintf("videocms:simple-upload:%d:%s", userID, requestKey))).String()
	}
	var session *models.UploadSession
	if uploadID != "" {
		var existing models.UploadSession
		err := h.Deps.DB.Unscoped().Where("user_id = ? AND client_upload_uuid = ?", userID, uploadID).First(&existing).Error
		if err == nil {
			if existing.Name != validation.Name || existing.Size != file.Size || existing.ParentFolderID != validation.ParentFolderID {
				return c.String(http.StatusConflict, "Idempotency key was already used for a different upload")
			}
			session = &existing
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return c.String(http.StatusInternalServerError, "Failed to inspect upload request")
		}
	}
	if session == nil {
		src, err := file.Open()
		if err != nil {
			c.Logger().Error("Failed to open uploaded file", err)
			return c.NoContent(http.StatusInternalServerError)
		}
		defer src.Close()

		status, staged, err := h.Logic.StageSimpleUploadWithID(validation.ParentFolderID, validation.Name, src, file.Size, userID, uploadID)
		if err != nil {
			return c.String(status, err.Error())
		}
		session = staged
	}
	if session.BackgroundJobID != "" {
		if existingJob, err := h.background().Job(c.Request().Context(), session.BackgroundJobID, &userID, false); err == nil {
			return acceptedBackgroundJob(c, &existingJob.Job)
		}
	}

	ownerID := userID
	job, _, err := h.background().Enqueue(c.Request().Context(), background.JobSpec{
		Kind: "media.ingest", Visibility: background.VisibilityUser, OwnerID: &ownerID,
		SubjectType: "upload_session", SubjectID: session.UUID,
		IdempotencyKey: fmt.Sprintf("upload:%d:%s", userID, session.ClientUploadUUID), Label: "Import " + session.Name,
		Tasks: []background.TaskSpec{{
			Kind: "media.import", Queue: background.QueueStorage, Phase: "Importing upload",
			Payload: map[string]any{"uploadSessionId": session.ID}, DedupeKey: fmt.Sprintf("upload:%d", session.ID),
			Priority: 40, Required: true, Weight: 20,
		}},
	})
	if err != nil {
		return c.String(http.StatusInternalServerError, "Failed to queue uploaded file")
	}
	if err := h.Deps.DB.Model(session).Update("background_job_id", job.ID).Error; err != nil {
		return c.String(http.StatusInternalServerError, "Failed to link uploaded file to background job")
	}
	return acceptedBackgroundJob(c, job)
}
