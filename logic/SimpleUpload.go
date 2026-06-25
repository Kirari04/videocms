package logic

import (
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func (s *Service) SimpleUpload(parentFolderID uint, name string, file io.Reader, fileSize int64, userID uint) (status int, response *models.Link, err error) {
	// check if user is blocked by another asynchronous user operation
	if s.Deps.RequestGate.Blocked(userID) {
		return http.StatusTooManyRequests, nil, errors.New("wait until the previous delete request finished")
	}
	s.Deps.RequestGate.Start(userID)
	defer s.Deps.RequestGate.End(userID)

	// check parent folder
	if parentFolderID > 0 {
		var count int64
		s.Deps.DB.Model(&models.Folder{}).Where("id = ?", parentFolderID).Count(&count)
		if count == 0 {
			return http.StatusBadRequest, nil, errors.New("parent folder doesn't exist")
		}
	}

	// check storage quota
	if status, err := s.CheckStorageQuota(userID, fileSize, ""); err != nil {
		return status, nil, err
	}

	// check upload session limit (to maintain consistency with resumable upload)
	user, err := s.GetModelUser(userID)
	if err != nil {
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}
	var activeUploadSessions int64
	s.Deps.DB.Model(&models.UploadSession{}).
		Where("user_id = ?", userID).
		Where("status IN ?", []string{
			models.UploadStatusCreated,
			models.UploadStatusUploading,
			models.UploadStatusUploaded,
			models.UploadStatusImporting,
			models.UploadStatusFailed,
		}).
		Distinct("client_upload_uuid").
		Count(&activeUploadSessions)
	if activeUploadSessions >= s.Config().MaxUploadSessions && activeUploadSessions >= user.Settings.UploadSessionsMax {
		return http.StatusBadRequest, nil, fmt.Errorf("exceeded max upload sessions")
	}

	// create temp file
	uploadUUID := uuid.NewString()
	tempPath := fmt.Sprintf("%s/%s.tmp", s.Config().FolderVideoUploadsPriv, uploadUUID)
	dst, err := os.Create(tempPath)
	if err != nil {
		log.Printf("Failed to create temp upload file: %v", err)
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}

	// ensure cleanup if something fails before CreateFile
	defer func() {
		if err != nil {
			os.Remove(tempPath)
		}
	}()

	// stream content
	written, err := io.Copy(dst, file)
	dst.Close()
	if err != nil {
		log.Printf("Failed to stream upload: %v", err)
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}

	// Verify size (security/integrity check)
	if written != fileSize {
		return http.StatusBadRequest, nil, fmt.Errorf("uploaded size mismatch")
	}

	// Create a dummy upload session for tracking (matches parity with chunked)
	session := models.UploadSession{
		Name:             name,
		UUID:             uploadUUID,
		ClientUploadUUID: uploadUUID,
		Protocol:         models.UploadProtocolSimple,
		Kind:             models.UploadKindSingle,
		Status:           models.UploadStatusUploaded,
		Size:             fileSize,
		Offset:           fileSize,
		QuotaBytes:       fileSize,
		PartCount:        1,
		StoragePath:      tempPath,
		ParentFolderID:   parentFolderID,
		UserID:           userID,
	}
	if err := s.Deps.DB.Create(&session).Error; err != nil {
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}

	// Track upload
	s.TrackUpload(userID, 0, session.ID, uint64(fileSize))

	// finalize with CreateFile
	status, dbLink, cloned, err := s.CreateFile(&tempPath, parentFolderID, name, uploadUUID, fileSize, userID, uploadUUID)

	// cleanup dummy session
	defer s.Deps.DB.Delete(&session)

	if err != nil {
		return status, nil, err
	}

	if cloned {
		os.Remove(tempPath)
	}

	// Update UploadLog with FileID
	s.Deps.DB.Model(&models.UploadLog{}).Where("upload_session_id = ?", session.ID).Update("file_id", dbLink.FileID)
	s.Deps.DB.Model(&session).Updates(map[string]interface{}{
		"status":  models.UploadStatusDone,
		"file_id": dbLink.FileID,
		"link_id": dbLink.ID,
	})

	return http.StatusOK, dbLink, nil
}
