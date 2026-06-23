package tusupload

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/tus/tusd/v2/pkg/filelocker"
	"github.com/tus/tusd/v2/pkg/filestore"
	tusd "github.com/tus/tusd/v2/pkg/handler"
	"gorm.io/gorm"
)

const (
	BasePath        = "/api/uploads/"
	retentionPeriod = 24 * time.Hour
)

type contextKey string

const (
	userIDContextKey   contextKey = "videocms_tus_user_id"
	adminContextKey    contextKey = "videocms_tus_admin"
	apiKeyIDContextKey contextKey = "videocms_tus_api_key_id"
)

var (
	server     *Server
	serverMu   sync.Mutex
	createMu   sync.Mutex
	cleanupMu  sync.Mutex
	activeStat = []string{
		models.UploadStatusCreated,
		models.UploadStatusUploading,
		models.UploadStatusUploaded,
		models.UploadStatusImporting,
		models.UploadStatusFailed,
	}
)

type Server struct {
	handler    http.Handler
	storageDir string
}

func GetServer() (*Server, error) {
	serverMu.Lock()
	defer serverMu.Unlock()

	if server != nil {
		return server, nil
	}

	storageDir := filepath.Join(config.ENV.FolderVideoUploadsPriv, "tus")
	if err := os.MkdirAll(storageDir, 0775); err != nil {
		return nil, err
	}

	store := filestore.New(storageDir)
	locker := filelocker.New(storageDir)
	composer := tusd.NewStoreComposer()
	store.UseIn(composer)
	locker.UseIn(composer)

	handler, err := tusd.NewHandler(tusd.Config{
		BasePath:                BasePath,
		StoreComposer:           composer,
		MaxSize:                 0,
		DisableDownload:         true,
		Cors:                    &tusd.CorsConfig{Disable: true},
		NotifyCreatedUploads:    true,
		NotifyUploadProgress:    true,
		NotifyCompleteUploads:   true,
		NotifyTerminatedUploads: true,
		UploadProgressInterval:  time.Second,
		PreUploadCreateCallback: preUploadCreate,
	})
	if err != nil {
		return nil, err
	}

	server = &Server{
		handler:    http.StripPrefix(strings.TrimSuffix(BasePath, "/"), handler),
		storageDir: storageDir,
	}
	server.consumeEvents(handler)
	return server, nil
}

func EchoHandler(c echo.Context) error {
	srv, err := GetServer()
	if err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	srv.ServeHTTP(c.Response(), c.Request())
	return nil
}

func StartCleanup() {
	if _, err := GetServer(); err != nil {
		log.Printf("[WARNING] failed to initialize tus upload server for cleanup: %v\n", err)
		return
	}
	CleanupExpiredOnce()
	ticker := time.NewTicker(time.Hour)
	for range ticker.C {
		CleanupExpiredOnce()
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var expiresAt *time.Time
	if r.Method != http.MethodOptions {
		ctx, status, message := authenticatedContext(r)
		if status != 0 {
			http.Error(w, message, status)
			return
		}
		r = r.WithContext(ctx)

		if uploadID := uploadIDFromPath(r.URL.Path); uploadID != "" {
			session, status, message := authorizeUploadResource(ctx, uploadID)
			if status != 0 {
				http.Error(w, message, status)
				return
			}
			expiresAt = session.ExpiresAt
		}
	}

	rw := &expirationResponseWriter{
		ResponseWriter: w,
		request:        r,
		expiresAt:      expiresAt,
	}
	s.handler.ServeHTTP(rw, r)
}

func authenticatedContext(r *http.Request) (context.Context, int, string) {
	bearer := r.Header.Get("Authorization")
	if bearer == "" {
		return r.Context(), http.StatusForbidden, "No JWT Token"
	}
	parts := strings.Split(bearer, " ")
	tokenString := parts[len(parts)-1]

	if strings.HasPrefix(tokenString, "ak_") {
		apiKey, err := auth.VerifyApiKey(inits.DB, tokenString)
		if err != nil {
			return r.Context(), http.StatusForbidden, "Invalid or Expired API Key"
		}
		go func(akID, uID uint, method, path, ip string) {
			now := time.Now()
			inits.DB.Model(&models.ApiKey{}).Where("id = ?", akID).Update("last_used_at", &now)
			inits.DB.Create(&models.ApiKeyAuditLog{
				ApiKeyID: akID,
				UserID:   uID,
				Method:   method,
				Path:     path,
				IP:       ip,
			})
		}(apiKey.ID, apiKey.UserID, r.Method, r.URL.Path, r.RemoteAddr)

		ctx := context.WithValue(r.Context(), userIDContextKey, apiKey.UserID)
		ctx = context.WithValue(ctx, adminContextKey, apiKey.User.Admin)
		ctx = context.WithValue(ctx, apiKeyIDContextKey, apiKey.ID)
		return ctx, 0, ""
	}

	token, claims, err := auth.VerifyJWT(tokenString)
	if err != nil {
		return r.Context(), http.StatusForbidden, "Invalid JWT Token"
	}
	if !token.Valid {
		return r.Context(), http.StatusForbidden, "Expired JWT Token"
	}
	ctx := context.WithValue(r.Context(), userIDContextKey, claims.UserID)
	ctx = context.WithValue(ctx, adminContextKey, claims.Admin)
	return ctx, 0, ""
}

func userIDFromContext(ctx context.Context) (uint, bool) {
	userID, ok := ctx.Value(userIDContextKey).(uint)
	return userID, ok
}

func uploadIDFromPath(path string) string {
	trimmed := strings.TrimPrefix(path, strings.TrimSuffix(BasePath, "/"))
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 || parts[0] == "" || parts[0] == "sessions" {
		return ""
	}
	if len(parts) > 1 && parts[1] == "finalize" {
		return ""
	}
	return parts[0]
}

func authorizeUploadResource(ctx context.Context, tusID string) (*models.UploadSession, int, string) {
	userID, ok := userIDFromContext(ctx)
	if !ok {
		return nil, http.StatusForbidden, "No JWT Token"
	}

	var session models.UploadSession
	err := inits.DB.Where("tus_id = ?", tusID).First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, http.StatusNotFound, "upload not found"
	}
	if err != nil {
		log.Printf("[WARNING] failed to authorize tus upload %s: %v\n", tusID, err)
		return nil, http.StatusInternalServerError, "failed to authorize upload"
	}
	if session.UserID != userID {
		return nil, http.StatusForbidden, "upload belongs to another user"
	}
	if isExpired(&session) {
		expireSession(&session)
		return nil, http.StatusGone, "upload expired"
	}
	return &session, 0, ""
}

func preUploadCreate(hook tusd.HookEvent) (tusd.HTTPResponse, tusd.FileInfoChanges, error) {
	createMu.Lock()
	defer createMu.Unlock()

	userID, ok := userIDFromContext(hook.Context)
	if !ok {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_AUTH_REQUIRED", "authentication required", http.StatusForbidden)
	}
	if !*config.ENV.UploadEnabled {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_UPLOAD_DISABLED", "uploads are disabled", http.StatusForbidden)
	}

	info := hook.Upload
	if info.Size <= 0 {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_INVALID_UPLOAD_LENGTH", "upload size must be greater than zero", http.StatusBadRequest)
	}
	if config.ENV.MaxUploadFilesize > 0 && info.Size > config.ENV.MaxUploadFilesize {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_MAX_SIZE_EXCEEDED", "maximum upload size exceeded", http.StatusRequestEntityTooLarge)
	}

	name := strings.TrimSpace(info.MetaData["filename"])
	if name == "" {
		name = strings.TrimSpace(info.MetaData["name"])
	}
	if name == "" || len(name) > 128 {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_INVALID_METADATA", "filename is required and must be at most 128 characters", http.StatusBadRequest)
	}

	parentFolderID, err := parseOptionalUint(info.MetaData["parent_folder_id"])
	if err != nil {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_INVALID_METADATA", "parent_folder_id must be a positive integer", http.StatusBadRequest)
	}
	if parentFolderID > 0 {
		var count int64
		if err := inits.DB.Model(&models.Folder{}).Where("id = ?", parentFolderID).Count(&count).Error; err != nil {
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_DATABASE", "failed to validate parent folder", http.StatusInternalServerError)
		}
		if count == 0 {
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_INVALID_PARENT", "parent folder doesn't exist", http.StatusBadRequest)
		}
	}

	clientUploadUUID := strings.TrimSpace(info.MetaData["client_upload_uuid"])
	if clientUploadUUID == "" {
		clientUploadUUID = uuid.NewString()
	}
	if _, err := uuid.Parse(clientUploadUUID); err != nil {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_INVALID_METADATA", "client_upload_uuid must be a UUID", http.StatusBadRequest)
	}

	kind := models.UploadKindSingle
	if info.IsPartial {
		kind = models.UploadKindPartial
	}
	if info.IsFinal {
		kind = models.UploadKindFinal
	}

	if status, err := enforceMaxUploadFileSize(userID, clientUploadUUID, kind, info.Size); err != nil {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_MAX_SIZE_EXCEEDED", err.Error(), status)
	}

	quotaBytes := info.Size
	if kind == models.UploadKindFinal {
		quotaBytes = 0
	}
	if quotaBytes > 0 {
		if status, err := logic.CheckStorageQuota(userID, quotaBytes, ""); err != nil {
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_QUOTA_EXCEEDED", err.Error(), status)
		}
	}
	if status, err := enforceActiveSessionLimit(userID, clientUploadUUID); err != nil {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_UPLOAD_SESSION_LIMIT", err.Error(), status)
	}

	tusID := uuid.NewString()
	expiresAt := time.Now().Add(retentionPeriod)
	session := models.UploadSession{
		UUID:             uuid.NewString(),
		ClientUploadUUID: clientUploadUUID,
		TusID:            tusID,
		Protocol:         models.UploadProtocolTus,
		Kind:             kind,
		Status:           models.UploadStatusCreated,
		Name:             name,
		Size:             info.Size,
		Offset:           0,
		QuotaBytes:       quotaBytes,
		PartCount:        len(info.PartialUploads),
		ParentFolderID:   parentFolderID,
		UserID:           userID,
		ExpiresAt:        &expiresAt,
	}

	if err := inits.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		if kind != models.UploadKindFinal {
			return nil
		}

		var partials []models.UploadSession
		if err := tx.Where("tus_id IN ?", info.PartialUploads).Find(&partials).Error; err != nil {
			return err
		}
		if len(partials) != len(info.PartialUploads) {
			return fmt.Errorf("one or more partial uploads were not found")
		}

		byTusID := map[string]models.UploadSession{}
		for _, partial := range partials {
			if partial.UserID != userID || partial.ClientUploadUUID != clientUploadUUID {
				return fmt.Errorf("partial upload belongs to another user or upload group")
			}
			if partial.Kind != models.UploadKindPartial {
				return fmt.Errorf("upload %s is not a partial upload", partial.TusID)
			}
			byTusID[partial.TusID] = partial
		}

		for index, partialTusID := range info.PartialUploads {
			partial := byTusID[partialTusID]
			if err := tx.Create(&models.UploadPart{
				UploadSessionID:        session.ID,
				PartialUploadSessionID: partial.ID,
				Index:                  index,
				TusID:                  partialTusID,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_UPLOAD_REJECTED", err.Error(), http.StatusBadRequest)
	}

	metadata := tusd.MetaData{}
	for key, value := range info.MetaData {
		metadata[key] = value
	}
	metadata["filename"] = name
	metadata["parent_folder_id"] = strconv.FormatUint(uint64(parentFolderID), 10)
	metadata["client_upload_uuid"] = clientUploadUUID

	return tusd.HTTPResponse{
			Header: tusd.HTTPHeader{
				"Upload-Expires": expiresAt.UTC().Format(http.TimeFormat),
			},
		},
		tusd.FileInfoChanges{
			ID:       tusID,
			MetaData: metadata,
		},
		nil
}

func enforceMaxUploadFileSize(userID uint, clientUploadUUID string, kind string, uploadSize int64) (int, error) {
	if config.ENV.MaxUploadFilesize <= 0 || kind != models.UploadKindPartial {
		return http.StatusOK, nil
	}

	var currentGroupSize int64
	if err := inits.DB.Model(&models.UploadSession{}).
		Where("user_id = ?", userID).
		Where("client_upload_uuid = ?", clientUploadUUID).
		Where("kind = ?", models.UploadKindPartial).
		Where("status IN ?", activeStat).
		Select("COALESCE(SUM(size), 0)").
		Scan(&currentGroupSize).Error; err != nil {
		return http.StatusInternalServerError, fmt.Errorf("failed to validate upload size")
	}

	if currentGroupSize+uploadSize > config.ENV.MaxUploadFilesize {
		return http.StatusRequestEntityTooLarge, fmt.Errorf("maximum upload size exceeded")
	}
	return http.StatusOK, nil
}

func enforceActiveSessionLimit(userID uint, clientUploadUUID string) (int, error) {
	user, err := helpers.GetUser(userID)
	if err != nil {
		return http.StatusInternalServerError, err
	}

	var activeUploadSessions int64
	query := inits.DB.Model(&models.UploadSession{}).
		Where("user_id = ?", userID).
		Where("client_upload_uuid <> ?", clientUploadUUID).
		Where("status IN ?", activeStat).
		Distinct("client_upload_uuid")
	if err := query.Count(&activeUploadSessions).Error; err != nil {
		return http.StatusInternalServerError, err
	}

	if activeUploadSessions >= config.ENV.MaxUploadSessions && activeUploadSessions >= user.Settings.UploadSessionsMax {
		return http.StatusBadRequest, fmt.Errorf("exceeded max upload sessions")
	}
	return http.StatusOK, nil
}

func parseOptionalUint(value string) (uint, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(parsed), nil
}

func (s *Server) consumeEvents(handler *tusd.Handler) {
	go func() {
		for event := range handler.CreatedUploads {
			updateSessionFromEvent(event, false)
		}
	}()
	go func() {
		for event := range handler.UploadProgress {
			updateSessionFromEvent(event, true)
		}
	}()
	go func() {
		for event := range handler.CompleteUploads {
			completeSessionFromEvent(event)
		}
	}()
	go func() {
		for event := range handler.TerminatedUploads {
			terminateSession(event.Upload.ID)
		}
	}()
}

func updateSessionFromEvent(event tusd.HookEvent, trackBytes bool) {
	db := inits.DB
	if db == nil {
		return
	}

	info := event.Upload
	updates := map[string]interface{}{
		"offset":       info.Offset,
		"storage_path": info.Storage[filestore.StorageKeyPath],
		"info_path":    info.Storage[filestore.StorageKeyInfoPath],
	}
	if info.Offset > 0 {
		updates["status"] = models.UploadStatusUploading
	}

	var session models.UploadSession
	if err := db.Where("tus_id = ?", info.ID).First(&session).Error; err != nil {
		return
	}
	if trackBytes && session.Kind != models.UploadKindFinal && info.Offset > session.Offset {
		helpers.TrackUpload(session.UserID, 0, session.ID, uint64(info.Offset-session.Offset))
	}
	db.Model(&session).Updates(updates)
}

func completeSessionFromEvent(event tusd.HookEvent) {
	db := inits.DB
	if db == nil {
		return
	}

	info := event.Upload
	now := time.Now()

	var session models.UploadSession
	if err := db.Where("tus_id = ?", info.ID).First(&session).Error; err != nil {
		return
	}
	if session.Kind != models.UploadKindFinal && info.Offset > session.Offset {
		helpers.TrackUpload(session.UserID, 0, session.ID, uint64(info.Offset-session.Offset))
	}

	db.Model(&session).Updates(map[string]interface{}{
		"status":       models.UploadStatusUploaded,
		"offset":       info.Offset,
		"storage_path": info.Storage[filestore.StorageKeyPath],
		"info_path":    info.Storage[filestore.StorageKeyInfoPath],
		"completed_at": &now,
	})
}

func terminateSession(tusID string) {
	db := inits.DB
	if db == nil {
		return
	}

	now := time.Now()
	var session models.UploadSession
	if err := db.Where("tus_id = ?", tusID).First(&session).Error; err != nil {
		return
	}
	db.Model(&session).Updates(map[string]interface{}{
		"status":     models.UploadStatusCanceled,
		"expires_at": &now,
	})
	db.Delete(&session)
}

func isExpired(session *models.UploadSession) bool {
	if session.ExpiresAt == nil {
		return false
	}
	if session.Status == models.UploadStatusDone || session.Status == models.UploadStatusCanceled || session.Status == models.UploadStatusExpired {
		return false
	}
	return time.Now().After(*session.ExpiresAt)
}

func expireSession(session *models.UploadSession) {
	if session.StoragePath != "" {
		_ = os.Remove(session.StoragePath)
	}
	if session.InfoPath != "" {
		_ = os.Remove(session.InfoPath)
	}
	now := time.Now()
	inits.DB.Model(session).Updates(map[string]interface{}{
		"status":     models.UploadStatusExpired,
		"expires_at": &now,
	})
	inits.DB.Delete(session)
}

func CleanupExpiredOnce() {
	cleanupMu.Lock()
	defer cleanupMu.Unlock()

	var sessions []models.UploadSession
	if err := inits.DB.Where("expires_at IS NOT NULL AND expires_at < ? AND status IN ?", time.Now(), activeStat).Find(&sessions).Error; err != nil {
		log.Printf("[WARNING] failed to find expired tus uploads: %v\n", err)
		return
	}
	for i := range sessions {
		expireSession(&sessions[i])
	}
	cleanupLegacyUploadSessions()
}

func ListSessions(userID uint) ([]models.UploadSessionsGetResponse, error) {
	var sessions []models.UploadSessionsGetResponse
	err := inits.DB.
		Model(&models.UploadSession{}).
		Where("user_id = ?", userID).
		Where("status IN ?", activeStat).
		Where("kind <> ?", models.UploadKindPartial).
		Order("created_at DESC").
		Find(&sessions).Error
	return sessions, err
}

func Finalize(uploadID string, userID uint) (int, *models.Link, error) {
	var session models.UploadSession
	if err := inits.DB.Unscoped().Where("tus_id = ?", uploadID).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return http.StatusNotFound, nil, fmt.Errorf("upload not found")
		}
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}

	if session.UserID != userID {
		return http.StatusForbidden, nil, echo.ErrForbidden
	}
	if session.Kind == models.UploadKindPartial {
		return http.StatusBadRequest, nil, fmt.Errorf("partial uploads cannot be finalized")
	}
	if session.LinkID > 0 {
		var link models.Link
		if err := inits.DB.First(&link, session.LinkID).Error; err != nil {
			return http.StatusInternalServerError, nil, echo.ErrInternalServerError
		}
		return http.StatusOK, &link, nil
	}
	if isExpired(&session) {
		expireSession(&session)
		return http.StatusGone, nil, fmt.Errorf("upload expired")
	}

	storagePath := session.StoragePath
	if storagePath == "" {
		storagePath = filepath.Join(config.ENV.FolderVideoUploadsPriv, "tus", session.TusID)
	}

	stat, err := os.Stat(storagePath)
	if err != nil {
		failFinalize(&session, fmt.Sprintf("uploaded file not found: %v", err))
		return http.StatusNotFound, nil, fmt.Errorf("uploaded file not found")
	}
	if stat.Size() != session.Size {
		failFinalize(&session, fmt.Sprintf("uploaded file size mismatch: server %d, expected %d", stat.Size(), session.Size))
		return http.StatusConflict, nil, fmt.Errorf("uploaded file size mismatch: server %d, expected %d", stat.Size(), session.Size)
	}

	if session.Status == models.UploadStatusImporting {
		return http.StatusConflict, nil, fmt.Errorf("upload is already importing")
	}
	if session.Status != models.UploadStatusUploaded && session.Status != models.UploadStatusFailed {
		if err := inits.DB.Model(&session).Updates(map[string]interface{}{
			"status": models.UploadStatusUploaded,
			"offset": session.Size,
		}).Error; err != nil {
			return http.StatusInternalServerError, nil, echo.ErrInternalServerError
		}
		session.Status = models.UploadStatusUploaded
		session.Offset = session.Size
	}

	if err := transitionToImporting(&session); err != nil {
		return http.StatusConflict, nil, err
	}

	fileUUID := uuid.NewString()
	destinationPath := filepath.Join(config.ENV.FolderVideoUploadsPriv, fileUUID+".tmp")
	if err := os.Rename(storagePath, destinationPath); err != nil {
		failFinalize(&session, fmt.Sprintf("failed to move uploaded file: %v", err))
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}

	status, link, cloned, err := logic.CreateFile(&destinationPath, session.ParentFolderID, session.Name, fileUUID, session.Size, userID, session.ClientUploadUUID)
	if err != nil {
		_ = os.Remove(destinationPath)
		failFinalize(&session, err.Error())
		return status, nil, err
	}
	if cloned {
		_ = os.Remove(destinationPath)
	}

	now := time.Now()
	if err := inits.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.UploadLog{}).
			Where("upload_session_id IN (?)",
				tx.Model(&models.UploadSession{}).Unscoped().Select("id").Where("client_upload_uuid = ? AND user_id = ?", session.ClientUploadUUID, userID),
			).
			Update("file_id", link.FileID).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.UploadSession{}).Unscoped().
			Where("client_upload_uuid = ? AND user_id = ?", session.ClientUploadUUID, userID).
			Updates(map[string]interface{}{
				"status":       models.UploadStatusDone,
				"file_id":      link.FileID,
				"link_id":      link.ID,
				"finalized_at": &now,
				"error":        "",
			}).Error; err != nil {
			return err
		}
		return tx.Where("client_upload_uuid = ? AND user_id = ?", session.ClientUploadUUID, userID).Delete(&models.UploadSession{}).Error
	}); err != nil {
		log.Printf("[WARNING] failed to update finalized upload session: %v\n", err)
	}

	removeTusFilesForGroup(session.ClientUploadUUID, userID)
	return http.StatusOK, link, nil
}

func transitionToImporting(session *models.UploadSession) error {
	if session.Status == models.UploadStatusImporting {
		return fmt.Errorf("upload is already importing")
	}
	if session.Status != models.UploadStatusUploaded && session.Status != models.UploadStatusFailed {
		return fmt.Errorf("upload is not complete")
	}

	res := inits.DB.Model(&models.UploadSession{}).
		Where("id = ? AND status IN ?", session.ID, []string{models.UploadStatusUploaded, models.UploadStatusFailed}).
		Update("status", models.UploadStatusImporting)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return fmt.Errorf("upload is already importing")
	}
	session.Status = models.UploadStatusImporting
	return nil
}

func failFinalize(session *models.UploadSession, message string) {
	inits.DB.Model(session).Updates(map[string]interface{}{
		"status": models.UploadStatusFailed,
		"error":  message,
	})
}

func removeTusFilesForGroup(clientUploadUUID string, userID uint) {
	var sessions []models.UploadSession
	if err := inits.DB.Unscoped().
		Where("client_upload_uuid = ? AND user_id = ?", clientUploadUUID, userID).
		Find(&sessions).Error; err != nil {
		return
	}
	for _, session := range sessions {
		if session.StoragePath != "" {
			_ = os.Remove(session.StoragePath)
		}
		if session.InfoPath != "" {
			_ = os.Remove(session.InfoPath)
		}
	}
}

func cleanupLegacyUploadSessions() {
	type legacyUploadSession struct {
		ID            uint
		SessionFolder string
	}

	var hasSessionFolder int
	if err := inits.DB.Raw("SELECT COUNT(*) FROM pragma_table_info('upload_sessions') WHERE name = 'session_folder'").Scan(&hasSessionFolder).Error; err != nil || hasSessionFolder == 0 {
		return
	}

	var sessions []legacyUploadSession
	err := inits.DB.Raw(`
		SELECT id, session_folder
		FROM upload_sessions
		WHERE (tus_id IS NULL OR tus_id = '')
		  AND (protocol IS NULL OR protocol = '')
		  AND deleted_at IS NULL
	`).Scan(&sessions).Error
	if err != nil {
		log.Printf("[WARNING] failed to find legacy upload sessions: %v\n", err)
		return
	}
	for _, session := range sessions {
		if session.SessionFolder != "" {
			_ = os.RemoveAll(session.SessionFolder)
		}
		now := time.Now()
		inits.DB.Model(&models.UploadSession{}).Where("id = ?", session.ID).Updates(map[string]interface{}{
			"status":     models.UploadStatusExpired,
			"expires_at": &now,
		})
		inits.DB.Delete(&models.UploadSession{}, session.ID)
	}
}

type expirationResponseWriter struct {
	http.ResponseWriter
	request   *http.Request
	expiresAt *time.Time
	wrote     bool
}

var _ interface {
	http.ResponseWriter
	Unwrap() http.ResponseWriter
} = (*expirationResponseWriter)(nil)

func (w *expirationResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *expirationResponseWriter) WriteHeader(statusCode int) {
	if w.wrote {
		return
	}
	w.wrote = true
	header := w.Header()
	if ext := header.Get("Tus-Extension"); ext != "" && !strings.Contains(ext, "expiration") {
		header.Set("Tus-Extension", ext+",expiration")
	}
	if config.ENV.MaxUploadFilesize > 0 && header.Get("Tus-Max-Size") == "" {
		header.Set("Tus-Max-Size", strconv.FormatInt(config.ENV.MaxUploadFilesize, 10))
	}
	if w.expiresAt != nil && statusCode < http.StatusBadRequest {
		header.Set("Upload-Expires", w.expiresAt.UTC().Format(http.TimeFormat))
	}
	if location := header.Get("Location"); location != "" {
		header.Set("Location", rewriteTusUploadURL(w.request, location))
	}
	if concat := header.Get("Upload-Concat"); concat != "" {
		header.Set("Upload-Concat", rewriteTusUploadConcatHeader(w.request, concat))
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *expirationResponseWriter) Write(data []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}
