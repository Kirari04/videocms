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

	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"

	"github.com/golang-jwt/jwt/v5"
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

type createFileFunc func(fromFile *string, toFolder uint, fileName string, fileID string, fileSize int64, userID uint, excludeSessionUUID string) (int, *models.Link, bool, error)

type Service struct {
	Deps  *app.Deps
	Auth  any
	Logic *logic.Service

	handler    http.Handler
	tusHandler *tusd.Handler
	storageDir string

	serverMu  sync.Mutex
	createMu  sync.Mutex
	cleanupMu sync.Mutex
	closeOnce sync.Once

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	createFile createFileFunc
}

type tusClaims struct {
	UserID   uint   `json:"userid"`
	Username string `json:"username"`
	Admin    bool   `json:"admin"`
	jwt.RegisteredClaims
}

func NewService(deps *app.Deps, authSvc any) *Service {
	if deps == nil {
		deps = &app.Deps{
			Snapshots: app.NewSnapshotStore(app.Snapshot{}),
		}
	}
	if deps.Snapshots == nil {
		deps.Snapshots = app.NewSnapshotStore(app.Snapshot{})
	}
	logicSvc := logic.NewService(deps)

	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		Deps:       deps,
		Auth:       authSvc,
		Logic:      logicSvc,
		ctx:        ctx,
		cancel:     cancel,
		createFile: logicSvc.CreateFile,
	}
}

func (s *Service) Config() config.Config {
	if s != nil && s.Deps != nil && s.Deps.Snapshots != nil {
		return s.Deps.Config()
	}
	return config.Config{}
}

func (s *Service) db() *gorm.DB {
	if s != nil && s.Deps != nil && s.Deps.DB != nil {
		return s.Deps.DB
	}
	return nil
}

func activeStatuses() []string {
	return []string{
		models.UploadStatusCreated,
		models.UploadStatusUploading,
		models.UploadStatusUploaded,
		models.UploadStatusImporting,
		models.UploadStatusFailed,
	}
}

func (s *Service) ensureHandler() error {
	s.serverMu.Lock()
	defer s.serverMu.Unlock()

	if s.handler != nil {
		return nil
	}

	storageDir := filepath.Join(s.Config().FolderVideoUploadsPriv, "tus")
	if err := os.MkdirAll(storageDir, 0775); err != nil {
		return err
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
		PreUploadCreateCallback: s.preUploadCreate,
	})
	if err != nil {
		return err
	}

	s.tusHandler = handler
	s.handler = http.StripPrefix(strings.TrimSuffix(BasePath, "/"), handler)
	s.storageDir = storageDir
	s.consumeEvents(handler)
	return nil
}

func (s *Service) EchoHandler(c echo.Context) error {
	if err := s.ensureHandler(); err != nil {
		c.Logger().Error(err)
		return c.NoContent(http.StatusInternalServerError)
	}

	s.ServeHTTP(c.Response(), c.Request())
	return nil
}

func (s *Service) StartCleanup(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}

	s.CleanupExpiredOnce()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.CleanupExpiredOnce()
		}
	}
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		s.cancel()
		s.wg.Wait()
	})
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := s.ensureHandler(); err != nil {
		log.Printf("[WARNING] failed to initialize tus upload server: %v\n", err)
		http.Error(w, "failed to initialize upload server", http.StatusInternalServerError)
		return
	}

	var expiresAt *time.Time
	if r.Method != http.MethodOptions {
		ctx, status, message := s.authenticatedContext(r)
		if status != 0 {
			http.Error(w, message, status)
			return
		}
		r = r.WithContext(ctx)

		if uploadID := uploadIDFromPath(r.URL.Path); uploadID != "" {
			session, status, message := s.authorizeUploadResource(ctx, uploadID)
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
		service:        s,
	}
	s.handler.ServeHTTP(rw, r)
}

func (s *Service) authenticatedContext(r *http.Request) (context.Context, int, string) {
	bearer := r.Header.Get("Authorization")
	if bearer == "" {
		return r.Context(), http.StatusForbidden, "No JWT Token"
	}
	parts := strings.Split(bearer, " ")
	tokenString := parts[len(parts)-1]

	if strings.HasPrefix(tokenString, "ak_") {
		db := s.db()
		if db == nil {
			return r.Context(), http.StatusInternalServerError, "database unavailable"
		}

		apiKey, err := s.verifyAPIKey(tokenString)
		if err != nil {
			return r.Context(), http.StatusForbidden, "Invalid or Expired API Key"
		}

		now := time.Now()
		db.Model(&models.ApiKey{}).Where("id = ?", apiKey.ID).Update("last_used_at", &now)
		db.Create(&models.ApiKeyAuditLog{
			ApiKeyID: apiKey.ID,
			UserID:   apiKey.UserID,
			Method:   r.Method,
			Path:     r.URL.Path,
			IP:       r.RemoteAddr,
		})

		ctx := context.WithValue(r.Context(), userIDContextKey, apiKey.UserID)
		ctx = context.WithValue(ctx, adminContextKey, apiKey.User.Admin)
		ctx = context.WithValue(ctx, apiKeyIDContextKey, apiKey.ID)
		return ctx, 0, ""
	}

	token, claims, err := s.verifyJWT(tokenString)
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

func (s *Service) verifyAPIKey(key string) (*models.ApiKey, error) {
	db := s.db()
	if db == nil {
		return nil, errors.New("database unavailable")
	}

	var apiKey models.ApiKey
	if err := db.Preload("User").Where("`key` = ?", key).First(&apiKey).Error; err != nil {
		return nil, err
	}
	if apiKey.ExpiresAt != nil && time.Now().After(*apiKey.ExpiresAt) {
		return nil, errors.New("api key expired")
	}
	return &apiKey, nil
}

func (s *Service) verifyJWT(tokenString string) (*jwt.Token, *tusClaims, error) {
	claims := &tusClaims{}
	key := []byte(s.Config().JwtSecretKey)
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return key, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return token, claims, nil
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

func (s *Service) authorizeUploadResource(ctx context.Context, tusID string) (*models.UploadSession, int, string) {
	userID, ok := userIDFromContext(ctx)
	if !ok {
		return nil, http.StatusForbidden, "No JWT Token"
	}

	db := s.db()
	if db == nil {
		return nil, http.StatusInternalServerError, "database unavailable"
	}

	var session models.UploadSession
	err := db.Where("tus_id = ?", tusID).First(&session).Error
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
		s.expireSession(&session)
		return nil, http.StatusGone, "upload expired"
	}
	return &session, 0, ""
}

func (s *Service) preUploadCreate(hook tusd.HookEvent) (tusd.HTTPResponse, tusd.FileInfoChanges, error) {
	s.createMu.Lock()
	defer s.createMu.Unlock()

	db := s.db()
	if db == nil {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_DATABASE", "database unavailable", http.StatusInternalServerError)
	}

	userID, ok := userIDFromContext(hook.Context)
	if !ok {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_AUTH_REQUIRED", "authentication required", http.StatusForbidden)
	}

	cfg := s.Config()
	if cfg.UploadEnabled == nil || !*cfg.UploadEnabled {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_UPLOAD_DISABLED", "uploads are disabled", http.StatusForbidden)
	}

	info := hook.Upload
	if info.Size <= 0 {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_INVALID_UPLOAD_LENGTH", "upload size must be greater than zero", http.StatusBadRequest)
	}
	if cfg.MaxUploadFilesize > 0 && info.Size > cfg.MaxUploadFilesize {
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
		if err := db.Model(&models.Folder{}).Where("id = ?", parentFolderID).Count(&count).Error; err != nil {
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

	if status, err := s.enforceMaxUploadFileSize(userID, clientUploadUUID, kind, info.Size); err != nil {
		return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_MAX_SIZE_EXCEEDED", err.Error(), status)
	}

	quotaBytes := info.Size
	if kind == models.UploadKindFinal {
		quotaBytes = 0
	}
	if quotaBytes > 0 {
		if status, err := s.checkStorageQuota(userID, quotaBytes, ""); err != nil {
			return tusd.HTTPResponse{}, tusd.FileInfoChanges{}, tusd.NewError("ERR_QUOTA_EXCEEDED", err.Error(), status)
		}
	}
	if status, err := s.enforceActiveSessionLimit(userID, clientUploadUUID); err != nil {
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

	if err := db.Transaction(func(tx *gorm.DB) error {
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

func (s *Service) enforceMaxUploadFileSize(userID uint, clientUploadUUID string, kind string, uploadSize int64) (int, error) {
	cfg := s.Config()
	if cfg.MaxUploadFilesize <= 0 || kind != models.UploadKindPartial {
		return http.StatusOK, nil
	}

	var currentGroupSize int64
	if err := s.db().Model(&models.UploadSession{}).
		Where("user_id = ?", userID).
		Where("client_upload_uuid = ?", clientUploadUUID).
		Where("kind = ?", models.UploadKindPartial).
		Where("status IN ?", activeStatuses()).
		Select("COALESCE(SUM(size), 0)").
		Scan(&currentGroupSize).Error; err != nil {
		return http.StatusInternalServerError, fmt.Errorf("failed to validate upload size")
	}

	if currentGroupSize+uploadSize > cfg.MaxUploadFilesize {
		return http.StatusRequestEntityTooLarge, fmt.Errorf("maximum upload size exceeded")
	}
	return http.StatusOK, nil
}

func (s *Service) enforceActiveSessionLimit(userID uint, clientUploadUUID string) (int, error) {
	var user models.User
	if err := s.db().First(&user, userID).Error; err != nil {
		return http.StatusInternalServerError, err
	}

	var activeUploadSessions int64
	query := s.db().Model(&models.UploadSession{}).
		Where("user_id = ?", userID).
		Where("client_upload_uuid <> ?", clientUploadUUID).
		Where("status IN ?", activeStatuses()).
		Distinct("client_upload_uuid")
	if err := query.Count(&activeUploadSessions).Error; err != nil {
		return http.StatusInternalServerError, err
	}

	cfg := s.Config()
	if activeUploadSessions >= cfg.MaxUploadSessions && activeUploadSessions >= user.Settings.UploadSessionsMax {
		return http.StatusBadRequest, fmt.Errorf("exceeded max upload sessions")
	}
	return http.StatusOK, nil
}

func (s *Service) checkStorageQuota(userID uint, additionalSize int64, excludeClientUploadUUID string) (int, error) {
	db := s.db()
	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		return http.StatusInternalServerError, errors.New("failed to fetch user")
	}

	if user.Storage == 0 {
		return http.StatusOK, nil
	}

	var usedStorage int64
	if err := db.Model(&models.Link{}).
		Joins("inner join files on files.id = links.file_id").
		Where("links.user_id = ?", userID).
		Select("COALESCE(SUM(files.size), 0)").
		Scan(&usedStorage).Error; err != nil {
		return http.StatusInternalServerError, errors.New("failed to calculate used storage")
	}

	var pendingStorage int64
	query := db.Model(&models.UploadSession{}).
		Where("user_id = ?", userID).
		Where("status IN ?", activeStatuses())
	if excludeClientUploadUUID != "" {
		query = query.Where("client_upload_uuid != ?", excludeClientUploadUUID)
	}
	if err := query.Select("COALESCE(SUM(quota_bytes), 0)").Scan(&pendingStorage).Error; err != nil {
		return http.StatusInternalServerError, errors.New("failed to calculate pending storage")
	}

	if usedStorage+pendingStorage+additionalSize > user.Storage {
		return http.StatusForbidden, fmt.Errorf("storage quota exceeded: %d/%d bytes used", usedStorage+pendingStorage, user.Storage)
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

func (s *Service) consumeEvents(handler *tusd.Handler) {
	s.consumeHookEvents(handler.CreatedUploads, func(event tusd.HookEvent) {
		s.updateSessionFromEvent(event, false)
	})
	s.consumeHookEvents(handler.UploadProgress, func(event tusd.HookEvent) {
		s.updateSessionFromEvent(event, true)
	})
	s.consumeHookEvents(handler.CompleteUploads, func(event tusd.HookEvent) {
		s.completeSessionFromEvent(event)
	})
	s.consumeHookEvents(handler.TerminatedUploads, func(event tusd.HookEvent) {
		s.terminateSession(event.Upload.ID)
	})
}

func (s *Service) consumeHookEvents(events <-chan tusd.HookEvent, consume func(tusd.HookEvent)) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-s.ctx.Done():
				return
			case event, ok := <-events:
				if !ok || s.ctx.Err() != nil {
					return
				}
				consume(event)
			}
		}
	}()
}

func (s *Service) updateSessionFromEvent(event tusd.HookEvent, trackBytes bool) {
	db := s.db()
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
		s.trackUpload(session.UserID, 0, session.ID, uint64(info.Offset-session.Offset))
	}
	db.Model(&session).Updates(updates)
}

func (s *Service) completeSessionFromEvent(event tusd.HookEvent) {
	db := s.db()
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
		s.trackUpload(session.UserID, 0, session.ID, uint64(info.Offset-session.Offset))
	}

	db.Model(&session).Updates(map[string]interface{}{
		"status":       models.UploadStatusUploaded,
		"offset":       info.Offset,
		"storage_path": info.Storage[filestore.StorageKeyPath],
		"info_path":    info.Storage[filestore.StorageKeyInfoPath],
		"completed_at": &now,
	})
}

func (s *Service) terminateSession(tusID string) {
	db := s.db()
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

func (s *Service) trackUpload(userID uint, fileID uint, uploadSessionID uint, bytes uint64) {
	if bytes == 0 {
		return
	}
	db := s.db()
	if db == nil {
		return
	}
	db.Create(&models.UploadLog{
		UserID:          userID,
		FileID:          fileID,
		UploadSessionID: uploadSessionID,
		Bytes:           bytes,
	})
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

func (s *Service) expireSession(session *models.UploadSession) {
	if session.StoragePath != "" {
		_ = os.Remove(session.StoragePath)
	}
	if session.InfoPath != "" {
		_ = os.Remove(session.InfoPath)
	}
	now := time.Now()
	db := s.db()
	db.Model(session).Updates(map[string]interface{}{
		"status":     models.UploadStatusExpired,
		"expires_at": &now,
	})
	db.Delete(session)
}

func (s *Service) CleanupExpiredOnce() {
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()

	db := s.db()
	if db == nil {
		return
	}

	var sessions []models.UploadSession
	if err := db.Where("expires_at IS NOT NULL AND expires_at < ? AND status IN ?", time.Now(), activeStatuses()).Find(&sessions).Error; err != nil {
		log.Printf("[WARNING] failed to find expired tus uploads: %v\n", err)
		return
	}
	for i := range sessions {
		s.expireSession(&sessions[i])
	}
	s.cleanupLegacyUploadSessions()
}

func (s *Service) ListSessions(userID uint) ([]models.UploadSessionsGetResponse, error) {
	db := s.db()
	if db == nil {
		return nil, errors.New("database unavailable")
	}

	var sessions []models.UploadSessionsGetResponse
	err := db.
		Model(&models.UploadSession{}).
		Where("user_id = ?", userID).
		Where("status IN ?", activeStatuses()).
		Where("kind <> ?", models.UploadKindPartial).
		Order("created_at DESC").
		Find(&sessions).Error
	return sessions, err
}

func (s *Service) Finalize(uploadID string, userID uint) (int, *models.Link, error) {
	db := s.db()
	if db == nil {
		return http.StatusInternalServerError, nil, errors.New("database unavailable")
	}

	var session models.UploadSession
	if err := db.Unscoped().Where("tus_id = ?", uploadID).First(&session).Error; err != nil {
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
		if err := db.First(&link, session.LinkID).Error; err != nil {
			return http.StatusInternalServerError, nil, echo.ErrInternalServerError
		}
		return http.StatusOK, &link, nil
	}
	if isExpired(&session) {
		s.expireSession(&session)
		return http.StatusGone, nil, fmt.Errorf("upload expired")
	}

	cfg := s.Config()
	storagePath := session.StoragePath
	if storagePath == "" {
		storagePath = filepath.Join(cfg.FolderVideoUploadsPriv, "tus", session.TusID)
	}

	stat, err := os.Stat(storagePath)
	if err != nil {
		s.failFinalize(&session, fmt.Sprintf("uploaded file not found: %v", err))
		return http.StatusNotFound, nil, fmt.Errorf("uploaded file not found")
	}
	if stat.Size() != session.Size {
		s.failFinalize(&session, fmt.Sprintf("uploaded file size mismatch: server %d, expected %d", stat.Size(), session.Size))
		return http.StatusConflict, nil, fmt.Errorf("uploaded file size mismatch: server %d, expected %d", stat.Size(), session.Size)
	}

	if session.Status == models.UploadStatusImporting {
		return http.StatusConflict, nil, fmt.Errorf("upload is already importing")
	}
	if session.Status != models.UploadStatusUploaded && session.Status != models.UploadStatusFailed {
		if err := db.Model(&session).Updates(map[string]interface{}{
			"status": models.UploadStatusUploaded,
			"offset": session.Size,
		}).Error; err != nil {
			return http.StatusInternalServerError, nil, echo.ErrInternalServerError
		}
		session.Status = models.UploadStatusUploaded
		session.Offset = session.Size
	}

	if err := s.transitionToImporting(&session); err != nil {
		return http.StatusConflict, nil, err
	}

	fileUUID := uuid.NewString()
	destinationPath := filepath.Join(cfg.FolderVideoUploadsPriv, fileUUID+".tmp")
	if err := os.Rename(storagePath, destinationPath); err != nil {
		s.failFinalize(&session, fmt.Sprintf("failed to move uploaded file: %v", err))
		return http.StatusInternalServerError, nil, echo.ErrInternalServerError
	}

	status, link, cloned, err := s.createFile(&destinationPath, session.ParentFolderID, session.Name, fileUUID, session.Size, userID, session.ClientUploadUUID)
	if err != nil {
		_ = os.Remove(destinationPath)
		s.failFinalize(&session, err.Error())
		return status, nil, err
	}
	if cloned {
		_ = os.Remove(destinationPath)
	}

	now := time.Now()
	if err := db.Transaction(func(tx *gorm.DB) error {
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

	s.removeTusFilesForGroup(session.ClientUploadUUID, userID)
	return http.StatusOK, link, nil
}

func (s *Service) transitionToImporting(session *models.UploadSession) error {
	if session.Status == models.UploadStatusImporting {
		return fmt.Errorf("upload is already importing")
	}
	if session.Status != models.UploadStatusUploaded && session.Status != models.UploadStatusFailed {
		return fmt.Errorf("upload is not complete")
	}

	res := s.db().Model(&models.UploadSession{}).
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

func (s *Service) failFinalize(session *models.UploadSession, message string) {
	s.db().Model(session).Updates(map[string]interface{}{
		"status": models.UploadStatusFailed,
		"error":  message,
	})
}

func (s *Service) removeTusFilesForGroup(clientUploadUUID string, userID uint) {
	var sessions []models.UploadSession
	if err := s.db().Unscoped().
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

func (s *Service) cleanupLegacyUploadSessions() {
	type legacyUploadSession struct {
		ID            uint
		SessionFolder string
	}

	db := s.db()
	var hasSessionFolder int
	if err := db.Raw("SELECT COUNT(*) FROM pragma_table_info('upload_sessions') WHERE name = 'session_folder'").Scan(&hasSessionFolder).Error; err != nil || hasSessionFolder == 0 {
		return
	}

	var sessions []legacyUploadSession
	err := db.Raw(`
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
		db.Model(&models.UploadSession{}).Where("id = ?", session.ID).Updates(map[string]interface{}{
			"status":     models.UploadStatusExpired,
			"expires_at": &now,
		})
		db.Delete(&models.UploadSession{}, session.ID)
	}
}

type expirationResponseWriter struct {
	http.ResponseWriter
	request   *http.Request
	expiresAt *time.Time
	service   *Service
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

	cfg := config.Config{}
	if w.service != nil {
		cfg = w.service.Config()
	}

	header := w.Header()
	if ext := header.Get("Tus-Extension"); ext != "" && !strings.Contains(ext, "expiration") {
		header.Set("Tus-Extension", ext+",expiration")
	}
	if cfg.MaxUploadFilesize > 0 && header.Get("Tus-Max-Size") == "" {
		header.Set("Tus-Max-Size", strconv.FormatInt(cfg.MaxUploadFilesize, 10))
	}
	if w.expiresAt != nil && statusCode < http.StatusBadRequest {
		header.Set("Upload-Expires", w.expiresAt.UTC().Format(http.TimeFormat))
	}
	if location := header.Get("Location"); location != "" {
		header.Set("Location", rewriteTusUploadURLWithBaseURL(w.request, location, cfg.BaseUrl))
	}
	if concat := header.Get("Upload-Concat"); concat != "" {
		header.Set("Upload-Concat", rewriteTusUploadConcatHeaderWithBaseURL(w.request, concat, cfg.BaseUrl))
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *expirationResponseWriter) Write(data []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}
