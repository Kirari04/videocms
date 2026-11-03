package tusupload

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/background"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/sharing"
	"gorm.io/gorm"
)

// FolderShareHeader selects a share-scoped guest identity on the standard tus
// endpoint. It never grants the visitor the owner's account permissions.
const FolderShareHeader = "X-Folder-Share"

func (s *Service) sharedUploadContext(r *http.Request, token string) (context.Context, int, string) {
	if s.db() == nil {
		return r.Context(), http.StatusInternalServerError, "database unavailable"
	}
	share, err := sharing.Resolve(s.db(), token)
	if err != nil {
		if errors.Is(err, sharing.ErrUnavailable) || errors.Is(err, gorm.ErrRecordNotFound) {
			return r.Context(), http.StatusNotFound, "this share is unavailable or has expired"
		}
		return r.Context(), http.StatusInternalServerError, "failed to load share"
	}
	if !share.AllowUpload {
		return r.Context(), http.StatusForbidden, "this link does not allow uploads"
	}
	if share.PasswordHash != "" || share.RequireCaptcha {
		cookie, err := r.Cookie(auth.FolderShareCookieName(share.Token))
		if err != nil || !s.Auth.VerifyFolderShareToken(cookie.Value, share) {
			return r.Context(), http.StatusUnauthorized, "unlock this share first"
		}
	}
	cfg := s.Config()
	if cfg.UploadEnabled == nil || !*cfg.UploadEnabled {
		return r.Context(), http.StatusForbidden, "uploads are disabled"
	}
	ctx := context.WithValue(r.Context(), userIDContextKey, share.UserID)
	return context.WithValue(ctx, folderShareContextKey, share), 0, ""
}

func (s *Service) validateSharedDestination(share *models.FolderShare, folderID uint) (int, error) {
	if !share.AllowRead && folderID != share.FolderID {
		return http.StatusForbidden, errors.New("upload to the shared destination only")
	}
	if err := sharing.ContainsFolder(s.db(), share, folderID); err != nil {
		return http.StatusForbidden, errors.New("folder is outside this share")
	}
	return 0, nil
}

func (s *Service) authorizeSharedSession(ctx context.Context, session *models.UploadSession) (int, error) {
	share, _ := ctx.Value(folderShareContextKey).(*models.FolderShare)
	if share == nil {
		return 0, nil // Account owners may manage their own guest upload sessions.
	}
	if session.FolderShareID != share.ID || session.FolderShareVersion != share.AccessVersion {
		return http.StatusForbidden, errors.New("upload is not available through this share")
	}
	return s.validateSharedDestination(share, session.ParentFolderID)
}

func (s *Service) validateUploadGroup(ctx context.Context, userID uint, group string, folderID uint, name string) (int, error) {
	shareID, version := uint(0), ""
	if share, _ := ctx.Value(folderShareContextKey).(*models.FolderShare); share != nil {
		shareID, version = share.ID, share.AccessVersion
	}
	var sessions []models.UploadSession
	if err := s.db().Unscoped().Where("user_id = ? AND client_upload_uuid = ?", userID, group).Find(&sessions).Error; err != nil {
		return http.StatusInternalServerError, errors.New("failed to load upload group")
	}
	for _, session := range sessions {
		if session.FolderShareID != shareID || session.FolderShareVersion != version || (shareID != 0 && (session.ParentFolderID != folderID || session.Name != name)) {
			return http.StatusForbidden, errors.New("upload group belongs to another destination or share")
		}
		if shareID != 0 && session.Status == models.UploadStatusDone {
			return http.StatusConflict, errors.New("this video has already been received")
		}
	}
	return 0, nil
}

// A guest reserves the whole video on its first partial resource. Subsequent
// parts must declare the same size, and their sum cannot exceed that reservation.
func (s *Service) validateSharedCreate(tx *gorm.DB, share *models.FolderShare, session *models.UploadSession, metadata map[string]string) (int, error) {
	expected := session.Size
	if value := metadata["file_size"]; value != "" {
		var err error
		expected, err = strconv.ParseInt(value, 10, 64)
		if err != nil || expected <= 0 {
			return http.StatusBadRequest, errors.New("file_size must be a positive integer")
		}
	} else if session.Kind == models.UploadKindPartial {
		return http.StatusBadRequest, errors.New("file_size is required for chunked video uploads")
	}
	if (session.Kind != models.UploadKindPartial && expected != session.Size) || session.Size > expected {
		return http.StatusBadRequest, errors.New("video size does not match file_size")
	}
	var group []models.UploadSession
	if err := tx.Where("user_id = ? AND client_upload_uuid = ? AND status IN ?", session.UserID, session.ClientUploadUUID, activeStatuses()).Find(&group).Error; err != nil {
		return http.StatusInternalServerError, errors.New("failed to validate video size")
	}
	var partialBytes int64
	for _, part := range group {
		if session.Kind == models.UploadKindSingle || (session.Kind == models.UploadKindPartial && part.Kind != models.UploadKindPartial) {
			return http.StatusConflict, errors.New("this upload group already has a complete video resource")
		}
		if part.ExpectedSize != expected {
			return http.StatusBadRequest, errors.New("video size changed within this upload")
		}
		if part.Kind == models.UploadKindPartial {
			if part.Size > expected-partialBytes {
				return http.StatusBadRequest, errors.New("chunks exceed the declared video size")
			}
			partialBytes += part.Size
		}
	}
	if session.Kind == models.UploadKindPartial && session.Size > expected-partialBytes {
		return http.StatusBadRequest, errors.New("chunks exceed the declared video size")
	}
	if session.Kind == models.UploadKindFinal && partialBytes != expected {
		return http.StatusBadRequest, errors.New("chunks do not match the declared video size")
	}
	session.ExpectedSize = expected
	return s.validateSharedCapacity(tx, share, expected, session.ClientUploadUUID)
}

func (s *Service) validateSharedCapacity(db *gorm.DB, share *models.FolderShare, size int64, excludeGroup string) (int, error) {
	if (share.MaxVideoSize > 0 && size > share.MaxVideoSize) || (s.Config().MaxUploadFilesize > 0 && size > s.Config().MaxUploadFilesize) {
		return http.StatusRequestEntityTooLarge, errors.New("video exceeds the maximum file size for this share")
	}
	if share.MaxFolderSize > 0 {
		capacity, err := sharing.UploadCapacity(db, share, excludeGroup)
		if err != nil {
			return http.StatusInternalServerError, errors.New("failed to check folder capacity")
		}
		if size > capacity.Remaining {
			return http.StatusInsufficientStorage, errors.New("folder size limit reached; remove videos or ask the owner to increase the limit")
		}
	}
	return 0, nil
}

type SharedUploadResult struct {
	Status    string
	PublicURL string `json:",omitempty"`
	Message   string `json:",omitempty"`
}

// SharedUploadStatus exposes only this guest upload's public video URL, after
// ingestion succeeds. It never exposes account job or upload-session details.
func (s *Service) SharedUploadStatus(r *http.Request, token, uploadID string) (SharedUploadResult, int, error) {
	ctx, status, message := s.sharedUploadContext(r, token)
	if status != 0 {
		return SharedUploadResult{}, status, errors.New(message)
	}
	share := ctx.Value(folderShareContextKey).(*models.FolderShare)
	if !share.ReturnsUploadLink() {
		return SharedUploadResult{}, http.StatusForbidden, errors.New("this share does not return public video links")
	}
	var session models.UploadSession
	if err := s.db().Unscoped().Where("tus_id = ? AND user_id = ?", uploadID, share.UserID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SharedUploadResult{}, http.StatusNotFound, errors.New("upload not found")
		}
		return SharedUploadResult{}, http.StatusInternalServerError, errors.New("failed to load upload")
	}
	if status, err := s.authorizeSharedSession(ctx, &session); err != nil {
		return SharedUploadResult{}, status, err
	}
	if session.Kind == models.UploadKindPartial {
		return SharedUploadResult{}, http.StatusBadRequest, errors.New("partial uploads have no public link")
	}
	if isExpired(&session) || session.Status == models.UploadStatusCanceled || session.Status == models.UploadStatusExpired {
		return SharedUploadResult{}, http.StatusGone, errors.New("upload is no longer available")
	}
	if session.Status == models.UploadStatusFailed {
		return SharedUploadResult{Status: "failed", Message: "The video could not be processed. Contact the folder owner."}, 0, nil
	}
	if session.Status != models.UploadStatusDone {
		return SharedUploadResult{Status: "processing"}, 0, nil
	}
	var link models.Link
	if err := s.db().Where("id = ? AND user_id = ?", session.LinkID, share.UserID).First(&link).Error; err != nil {
		return SharedUploadResult{}, http.StatusNotFound, errors.New("video is no longer available")
	}
	if err := sharing.ContainsFolder(s.db(), share, link.ParentFolderID); err != nil {
		return SharedUploadResult{}, http.StatusForbidden, errors.New("video is outside this share")
	}
	return SharedUploadResult{Status: "ready", PublicURL: "/v/" + link.UUID}, 0, nil
}

func validSharedFilename(name string) bool {
	return slices.Contains(config.EXTENSIONS, strings.ToLower(strings.TrimPrefix(filepath.Ext(name), ".")))
}

// EnqueueSharedFinalize rechecks live share access after all chunks arrive and
// returns only to the controller, which sends a receipt without owner job URLs.
func (s *Service) EnqueueSharedFinalize(r *http.Request, token, uploadID string) (*background.Job, bool, int, error) {
	ctx, status, message := s.sharedUploadContext(r, token)
	if status != 0 {
		return nil, false, status, errors.New(message)
	}
	userID, _ := userIDFromContext(ctx)
	return s.EnqueueFinalize(ctx, uploadID, userID)
}
