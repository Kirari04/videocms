package logic

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"ch/kirari04/videocms/models"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

// StageSimpleUpload durably stores the request body and creates the same upload
// session used by resumable uploads. Importing is deliberately left to the
// background runtime.
func (s *Service) StageSimpleUpload(parentFolderID uint, name string, file io.Reader, fileSize int64, userID uint) (int, *models.UploadSession, error) {
	return s.StageSimpleUploadWithID(parentFolderID, name, file, fileSize, userID, "")
}

func (s *Service) StageSimpleUploadWithID(parentFolderID uint, name string, file io.Reader, fileSize int64, userID uint, uploadID string) (int, *models.UploadSession, error) {
	if !s.Deps.RequestGate.TryStart(userID) {
		return http.StatusTooManyRequests, nil, errors.New("wait until the previous delete request finished")
	}
	defer s.Deps.RequestGate.End(userID)

	if status, err := ValidateFolderDestination(s.Deps.DB, parentFolderID, userID); err != nil {
		return status, nil, err
	}
	if status, err := s.CheckStorageQuota(userID, fileSize, ""); err != nil {
		return status, nil, err
	}
	user, err := s.GetModelUser(userID)
	if err != nil {
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}
	var active int64
	if err := s.Deps.DB.Model(&models.UploadSession{}).
		Where("user_id = ? AND status IN ?", userID, []string{models.UploadStatusCreated, models.UploadStatusUploading, models.UploadStatusUploaded, models.UploadStatusImporting, models.UploadStatusFailed}).
		Distinct("client_upload_uuid").Count(&active).Error; err != nil {
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}
	if active >= s.Config().MaxUploadSessions && active >= user.Settings.UploadSessionsMax {
		return http.StatusBadRequest, nil, errors.New("exceeded max upload sessions")
	}

	if uploadID == "" {
		uploadID = uuid.NewString()
	}
	tempPath := fmt.Sprintf("%s/%s.tmp", s.Config().FolderVideoUploadsPriv, uploadID)
	destination, err := os.Create(tempPath)
	if err != nil {
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}
	written, copyErr := io.Copy(destination, file)
	closeErr := destination.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tempPath)
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}
	if written != fileSize {
		_ = os.Remove(tempPath)
		return http.StatusBadRequest, nil, errors.New("uploaded size mismatch")
	}
	session := &models.UploadSession{
		Name: name, UUID: uploadID, ClientUploadUUID: uploadID, Protocol: models.UploadProtocolSimple,
		Kind: models.UploadKindSingle, Status: models.UploadStatusUploaded, Size: fileSize, Offset: fileSize,
		QuotaBytes: fileSize, PartCount: 1, StoragePath: tempPath, ParentFolderID: parentFolderID, UserID: userID,
	}
	status := http.StatusInternalServerError
	if err := s.Deps.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if status, err = ValidateFolderDestination(tx, parentFolderID, userID); err != nil {
			return err
		}
		status = http.StatusInternalServerError
		return tx.Create(session).Error
	}); err != nil {
		_ = os.Remove(tempPath)
		return status, nil, err
	}
	s.TrackUpload(userID, 0, session.ID, uint64(fileSize))
	return http.StatusAccepted, session, nil
}
