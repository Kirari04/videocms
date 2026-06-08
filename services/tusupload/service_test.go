package tusupload

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"

	"github.com/google/uuid"
	tusd "github.com/tus/tusd/v2/pkg/handler"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type tusTestEnv struct {
	db    *gorm.DB
	user  models.User
	token string
	root  string
}

func setupTusTest(t *testing.T) tusTestEnv {
	t.Helper()

	oldDB := inits.DB
	oldENV := config.ENV
	serverMu.Lock()
	oldServer := server
	server = nil
	serverMu.Unlock()

	root := t.TempDir()
	uploads := filepath.Join(root, "uploads")
	qualitys := filepath.Join(root, "qualitys")
	if err := os.MkdirAll(uploads, 0775); err != nil {
		t.Fatalf("create upload dir: %v", err)
	}
	if err := os.MkdirAll(qualitys, 0775); err != nil {
		t.Fatalf("create quality dir: %v", err)
	}

	uploadEnabled := true
	config.ENV = config.Config{
		JwtSecretKey:                 "test-jwt-secret",
		JwtMediaSecretKey:            "test-media-secret",
		UploadEnabled:                &uploadEnabled,
		MaxUploadFilesize:            1024 * 1024,
		MaxUploadChunkSize:           64 * 1024,
		MaxUploadSessions:            10,
		FolderVideoUploadsPriv:       uploads,
		FolderVideoQualitysPriv:      qualitys,
		FolderVideoQualitysPub:       "/videos/qualitys",
		DownloadEnabled:              &uploadEnabled,
		EncodingEnabled:              &uploadEnabled,
		ContinueWatchingPopupEnabled: &uploadEnabled,
		PlayerV2Enabled:              &uploadEnabled,
		RemoteDownloadEnabled:        &uploadEnabled,
		CaptchaEnabled:               &uploadEnabled,
		CaptchaLoginEnabled:          &uploadEnabled,
		CaptchaPlayerEnabled:         &uploadEnabled,
		CorsAllowCredentials:         &uploadEnabled,
	}

	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logger.Silent),
		DisableForeignKeyConstraintWhenMigrating: true,
		IgnoreRelationshipsWhenMigrating:         true,
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	inits.DB = db

	if err := db.AutoMigrate(
		&models.User{},
		&models.Folder{},
		&models.File{},
		&models.Link{},
		&models.UploadSession{},
		&models.UploadPart{},
		&models.UploadLog{},
		&models.ApiKey{},
		&models.ApiKeyAuditLog{},
	); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}

	user := models.User{
		Username: "tester",
		Storage:  0,
		Settings: models.UserSettings{
			UploadSessionsMax: 10,
		},
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, _, err := auth.GenerateJWT(user)
	if err != nil {
		t.Fatalf("generate jwt: %v", err)
	}

	t.Cleanup(func() {
		serverMu.Lock()
		server = oldServer
		serverMu.Unlock()
		inits.DB = oldDB
		config.ENV = oldENV
	})

	return tusTestEnv{
		db:    db,
		user:  user,
		token: token,
		root:  root,
	}
}

func TestTusCreateRequiresAuthentication(t *testing.T) {
	setupTusTest(t)
	srv, err := GetServer()
	if err != nil {
		t.Fatalf("get server: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/uploads", nil)
	req.Header.Set("Tus-Resumable", "1.0.0")
	req.Header.Set("Upload-Length", "10")
	req.Header.Set("Upload-Metadata", tusMetadata(map[string]string{"filename": "movie.mp4"}))
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTusCreateRejectsDisabledUploads(t *testing.T) {
	env := setupTusTest(t)
	disabled := false
	config.ENV.UploadEnabled = &disabled

	rec := createTusUploadRequest(t, env.token, 10, map[string]string{"filename": "movie.mp4"})

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTusCreateRejectsOversizedUploads(t *testing.T) {
	env := setupTusTest(t)
	config.ENV.MaxUploadFilesize = 4

	rec := createTusUploadRequest(t, env.token, 5, map[string]string{"filename": "movie.mp4"})

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTusCreateUsesUpdatedMaxUploadFilesize(t *testing.T) {
	env := setupTusTest(t)
	config.ENV.MaxUploadFilesize = 4
	if _, err := GetServer(); err != nil {
		t.Fatalf("get server: %v", err)
	}

	config.ENV.MaxUploadFilesize = 10
	rec := createTusUploadRequest(t, env.token, 8, map[string]string{"filename": "movie.mp4"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 after increasing max upload size, got %d: %s", rec.Code, rec.Body.String())
	}
	tusID := path.Base(rec.Header().Get("Location"))
	waitFor(t, func() bool {
		var session models.UploadSession
		if err := env.db.Where("tus_id = ?", tusID).First(&session).Error; err != nil {
			return false
		}
		return session.StoragePath != "" && session.InfoPath != ""
	})

	options := httptest.NewRequest(http.MethodOptions, "/api/uploads", nil)
	optionsRec := httptest.NewRecorder()
	srv, err := GetServer()
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	srv.ServeHTTP(optionsRec, options)
	if optionsRec.Code != http.StatusNoContent && optionsRec.Code != http.StatusOK {
		t.Fatalf("expected OPTIONS 204 or 200, got %d: %s", optionsRec.Code, optionsRec.Body.String())
	}
	if maxSize := optionsRec.Header().Get("Tus-Max-Size"); maxSize != "10" {
		t.Fatalf("expected dynamic Tus-Max-Size 10, got %q", maxSize)
	}
}

func TestRewriteTusUploadURLUsesForwardedHTTPS(t *testing.T) {
	setupTusTest(t)

	req := httptest.NewRequest(http.MethodPost, "http://videocms.senpai.one/api/uploads", nil)
	req.Header.Set("X-Forwarded-Proto", "https,http")

	got := rewriteTusUploadURL(req, "http://videocms.senpai.one/api/uploads/upload-id")
	want := "https://videocms.senpai.one/api/uploads/upload-id"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestRewriteTusUploadURLUsesBaseURLForInternalHost(t *testing.T) {
	setupTusTest(t)
	config.ENV.BaseUrl = "https://videocms.senpai.one"

	req := httptest.NewRequest(http.MethodPost, "http://videocms:3000/api/uploads", nil)

	got := rewriteTusUploadURL(req, "http://videocms:3000/api/uploads/upload-id")
	want := "https://videocms.senpai.one/api/uploads/upload-id"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestRewriteTusUploadURLLeavesNonUploadLocationUnchanged(t *testing.T) {
	setupTusTest(t)

	req := httptest.NewRequest(http.MethodPost, "http://videocms.senpai.one/api/uploads", nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	location := "http://videocms.senpai.one/api/other/upload-id"
	if got := rewriteTusUploadURL(req, location); got != location {
		t.Fatalf("expected non-upload location to remain %q, got %q", location, got)
	}
}

func TestRewriteTusUploadURLIgnoresForwardedHost(t *testing.T) {
	setupTusTest(t)

	req := httptest.NewRequest(http.MethodPost, "http://videocms.senpai.one/api/uploads", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "evil.example")

	got := rewriteTusUploadURL(req, "http://videocms.senpai.one/api/uploads/upload-id")
	want := "https://videocms.senpai.one/api/uploads/upload-id"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestRewriteTusUploadConcatHeader(t *testing.T) {
	setupTusTest(t)

	req := httptest.NewRequest(http.MethodHead, "http://videocms.senpai.one/api/uploads/final-id", nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	got := rewriteTusUploadConcatHeader(req, "final;http://videocms.senpai.one/api/uploads/one http://videocms.senpai.one/api/uploads/two")
	want := "final;https://videocms.senpai.one/api/uploads/one https://videocms.senpai.one/api/uploads/two"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestTusCreateLocationUsesForwardedHTTPS(t *testing.T) {
	env := setupTusTest(t)
	srv, err := GetServer()
	if err != nil {
		t.Fatalf("get server: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://videocms.senpai.one/api/uploads", nil)
	setTusAuthHeaders(req, env.token)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Upload-Length", "10")
	req.Header.Set("Upload-Metadata", tusMetadata(map[string]string{"filename": "movie.mp4"}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, "https://videocms.senpai.one/api/uploads/") {
		t.Fatalf("expected HTTPS public Location, got %q", location)
	}
	waitForTusStoragePath(t, env, path.Base(location))
}

func TestTusCreateLocationUsesBaseURLForInternalHost(t *testing.T) {
	env := setupTusTest(t)
	config.ENV.BaseUrl = "https://videocms.senpai.one"
	srv, err := GetServer()
	if err != nil {
		t.Fatalf("get server: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://videocms:3000/api/uploads", nil)
	setTusAuthHeaders(req, env.token)
	req.Header.Set("Upload-Length", "10")
	req.Header.Set("Upload-Metadata", tusMetadata(map[string]string{"filename": "movie.mp4"}))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, "https://videocms.senpai.one/api/uploads/") {
		t.Fatalf("expected BaseUrl public Location, got %q", location)
	}
	waitForTusStoragePath(t, env, path.Base(location))
}

func TestTusCreateRejectsInvalidParentFolder(t *testing.T) {
	env := setupTusTest(t)

	rec := createTusUploadRequest(t, env.token, 10, map[string]string{
		"filename":         "movie.mp4",
		"parent_folder_id": "999",
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPreUploadCreateFinalConcatenationUsesZeroQuotaAndRecordsParts(t *testing.T) {
	env := setupTusTest(t)
	clientUploadUUID := uuid.NewString()
	partials := []models.UploadSession{
		{
			UUID:             uuid.NewString(),
			ClientUploadUUID: clientUploadUUID,
			TusID:            "partial-one",
			Protocol:         models.UploadProtocolTus,
			Kind:             models.UploadKindPartial,
			Status:           models.UploadStatusUploaded,
			Name:             "movie.mp4",
			Size:             5,
			QuotaBytes:       5,
			UserID:           env.user.ID,
		},
		{
			UUID:             uuid.NewString(),
			ClientUploadUUID: clientUploadUUID,
			TusID:            "partial-two",
			Protocol:         models.UploadProtocolTus,
			Kind:             models.UploadKindPartial,
			Status:           models.UploadStatusUploaded,
			Name:             "movie.mp4",
			Size:             7,
			QuotaBytes:       7,
			UserID:           env.user.ID,
		},
	}
	if err := env.db.Create(&partials).Error; err != nil {
		t.Fatalf("create partial sessions: %v", err)
	}

	ctx := context.WithValue(context.Background(), userIDContextKey, env.user.ID)
	_, changes, err := preUploadCreate(tusd.HookEvent{
		Context: ctx,
		Upload: tusd.FileInfo{
			Size:           12,
			IsFinal:        true,
			PartialUploads: []string{"partial-one", "partial-two"},
			MetaData: tusd.MetaData{
				"filename":           "movie.mp4",
				"client_upload_uuid": clientUploadUUID,
			},
		},
	})
	if err != nil {
		t.Fatalf("preUploadCreate returned error: %v", err)
	}
	if changes.ID == "" {
		t.Fatal("expected generated tus id")
	}

	var final models.UploadSession
	if err := env.db.Where("tus_id = ?", changes.ID).First(&final).Error; err != nil {
		t.Fatalf("load final session: %v", err)
	}
	if final.Kind != models.UploadKindFinal {
		t.Fatalf("expected final kind, got %q", final.Kind)
	}
	if final.QuotaBytes != 0 {
		t.Fatalf("expected final quota to be zero, got %d", final.QuotaBytes)
	}
	if final.PartCount != 2 {
		t.Fatalf("expected part count 2, got %d", final.PartCount)
	}

	var parts []models.UploadPart
	if err := env.db.Where("upload_session_id = ?", final.ID).Order("`index`").Find(&parts).Error; err != nil {
		t.Fatalf("load upload parts: %v", err)
	}
	if len(parts) != 2 || parts[0].TusID != "partial-one" || parts[1].TusID != "partial-two" {
		t.Fatalf("unexpected upload parts: %#v", parts)
	}
}

func TestTusHTTPFinalConcatenationRecordsParts(t *testing.T) {
	env := setupTusTest(t)
	srv, err := GetServer()
	if err != nil {
		t.Fatalf("get server: %v", err)
	}

	clientUploadUUID := uuid.NewString()
	payloads := [][]byte{[]byte("hello"), []byte("world!!")}
	partialLocations := make([]string, 0, len(payloads))

	for _, payload := range payloads {
		create := httptest.NewRequest(http.MethodPost, "/api/uploads", nil)
		setTusAuthHeaders(create, env.token)
		create.Header.Set("Upload-Length", strconv.Itoa(len(payload)))
		create.Header.Set("Upload-Concat", "partial")
		create.Header.Set("Upload-Metadata", tusMetadata(map[string]string{
			"filename":           "movie.mp4",
			"client_upload_uuid": clientUploadUUID,
		}))
		createRec := httptest.NewRecorder()
		srv.ServeHTTP(createRec, create)
		if createRec.Code != http.StatusCreated {
			t.Fatalf("expected partial create 201, got %d: %s", createRec.Code, createRec.Body.String())
		}

		location := createRec.Header().Get("Location")
		if location == "" {
			t.Fatal("expected partial Location header")
		}
		partialLocations = append(partialLocations, location)

		patch := httptest.NewRequest(http.MethodPatch, location, bytes.NewReader(payload))
		setTusAuthHeaders(patch, env.token)
		patch.Header.Set("Content-Type", "application/offset+octet-stream")
		patch.Header.Set("Upload-Offset", "0")
		patch.ContentLength = int64(len(payload))
		patchRec := httptest.NewRecorder()
		srv.ServeHTTP(patchRec, patch)
		if patchRec.Code != http.StatusNoContent {
			t.Fatalf("expected partial PATCH 204, got %d: %s", patchRec.Code, patchRec.Body.String())
		}
	}

	finalCreate := httptest.NewRequest(http.MethodPost, "/api/uploads", nil)
	setTusAuthHeaders(finalCreate, env.token)
	finalCreate.Header.Set("Upload-Concat", "final;"+strings.Join(partialLocations, " "))
	finalCreate.Header.Set("Upload-Metadata", tusMetadata(map[string]string{
		"filename":           "movie.mp4",
		"client_upload_uuid": clientUploadUUID,
	}))
	finalRec := httptest.NewRecorder()
	srv.ServeHTTP(finalRec, finalCreate)
	if finalRec.Code != http.StatusCreated {
		t.Fatalf("expected final create 201, got %d: %s", finalRec.Code, finalRec.Body.String())
	}

	finalTusID := path.Base(finalRec.Header().Get("Location"))
	var final models.UploadSession
	waitFor(t, func() bool {
		return env.db.Where("tus_id = ?", finalTusID).First(&final).Error == nil
	})
	if final.Kind != models.UploadKindFinal {
		t.Fatalf("expected final kind, got %q", final.Kind)
	}
	if final.PartCount != len(payloads) {
		t.Fatalf("expected part count %d, got %d", len(payloads), final.PartCount)
	}
	if final.UserID != env.user.ID {
		t.Fatalf("expected user id %d, got %d", env.user.ID, final.UserID)
	}

	var parts []models.UploadPart
	if err := env.db.Where("upload_session_id = ?", final.ID).Order("`index`").Find(&parts).Error; err != nil {
		t.Fatalf("load upload parts: %v", err)
	}
	if len(parts) != len(payloads) {
		t.Fatalf("expected %d upload parts, got %d", len(payloads), len(parts))
	}
	for index, part := range parts {
		if part.Index != index {
			t.Fatalf("expected part index %d, got %d", index, part.Index)
		}
		if part.TusID != path.Base(partialLocations[index]) {
			t.Fatalf("expected part tus id %q, got %q", path.Base(partialLocations[index]), part.TusID)
		}
	}
}

func TestPreUploadCreateRejectsPartialGroupOverMaxFileSize(t *testing.T) {
	env := setupTusTest(t)
	config.ENV.MaxUploadFilesize = 10
	clientUploadUUID := uuid.NewString()
	existingPartial := models.UploadSession{
		UUID:             uuid.NewString(),
		ClientUploadUUID: clientUploadUUID,
		TusID:            "partial-one",
		Protocol:         models.UploadProtocolTus,
		Kind:             models.UploadKindPartial,
		Status:           models.UploadStatusUploaded,
		Name:             "movie.mp4",
		Size:             7,
		QuotaBytes:       7,
		UserID:           env.user.ID,
	}
	if err := env.db.Create(&existingPartial).Error; err != nil {
		t.Fatalf("create existing partial: %v", err)
	}

	ctx := context.WithValue(context.Background(), userIDContextKey, env.user.ID)
	_, _, err := preUploadCreate(tusd.HookEvent{
		Context: ctx,
		Upload: tusd.FileInfo{
			Size:      4,
			IsPartial: true,
			MetaData: tusd.MetaData{
				"filename":           "movie.mp4",
				"client_upload_uuid": clientUploadUUID,
			},
		},
	})
	if err == nil {
		t.Fatal("expected partial group over max file size to be rejected")
	}

	var tusErr tusd.Error
	if !errors.As(err, &tusErr) {
		t.Fatalf("expected tusd error, got %T: %v", err, err)
	}
	if tusErr.HTTPResponse.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", tusErr.HTTPResponse.StatusCode, tusErr.Error())
	}
}

func TestAuthorizeUploadResourceRejectsWrongOwnerAndExpiredUploads(t *testing.T) {
	env := setupTusTest(t)
	other := models.User{Username: "other"}
	if err := env.db.Create(&other).Error; err != nil {
		t.Fatalf("create other user: %v", err)
	}

	session := models.UploadSession{
		UUID:             uuid.NewString(),
		ClientUploadUUID: uuid.NewString(),
		TusID:            "owned-upload",
		Protocol:         models.UploadProtocolTus,
		Kind:             models.UploadKindSingle,
		Status:           models.UploadStatusCreated,
		Name:             "movie.mp4",
		Size:             10,
		QuotaBytes:       10,
		UserID:           env.user.ID,
	}
	if err := env.db.Create(&session).Error; err != nil {
		t.Fatalf("create upload session: %v", err)
	}

	ctx := context.WithValue(context.Background(), userIDContextKey, other.ID)
	if _, status, _ := authorizeUploadResource(ctx, session.TusID); status != http.StatusForbidden {
		t.Fatalf("expected wrong owner status 403, got %d", status)
	}

	expiredAt := time.Now().Add(-time.Minute)
	expired := models.UploadSession{
		UUID:             uuid.NewString(),
		ClientUploadUUID: uuid.NewString(),
		TusID:            "expired-upload",
		Protocol:         models.UploadProtocolTus,
		Kind:             models.UploadKindSingle,
		Status:           models.UploadStatusCreated,
		Name:             "movie.mp4",
		Size:             10,
		QuotaBytes:       10,
		UserID:           other.ID,
		ExpiresAt:        &expiredAt,
	}
	if err := env.db.Create(&expired).Error; err != nil {
		t.Fatalf("create expired upload session: %v", err)
	}

	ctx = context.WithValue(context.Background(), userIDContextKey, other.ID)
	if _, status, _ := authorizeUploadResource(ctx, expired.TusID); status != http.StatusGone {
		t.Fatalf("expected expired status 410, got %d", status)
	}

	var stored models.UploadSession
	if err := env.db.Unscoped().Where("tus_id = ?", expired.TusID).First(&stored).Error; err != nil {
		t.Fatalf("load expired session: %v", err)
	}
	if stored.Status != models.UploadStatusExpired {
		t.Fatalf("expected expired status in db, got %q", stored.Status)
	}
	if stored.DeletedAt == nil || !stored.DeletedAt.Valid {
		t.Fatal("expected expired upload session to be soft deleted")
	}
}

func TestCheckStorageQuotaUsesQuotaBytesAndCanExcludeClientGroup(t *testing.T) {
	env := setupTusTest(t)
	if err := env.db.Model(&env.user).Update("storage", int64(100)).Error; err != nil {
		t.Fatalf("update storage quota: %v", err)
	}

	file := models.File{
		UUID:   uuid.NewString(),
		Size:   40,
		UserID: env.user.ID,
	}
	if err := env.db.Create(&file).Error; err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := env.db.Create(&models.Link{
		UUID:   uuid.NewString(),
		Name:   "existing.mp4",
		FileID: file.ID,
		UserID: env.user.ID,
	}).Error; err != nil {
		t.Fatalf("create link: %v", err)
	}

	currentGroup := uuid.NewString()
	sessions := []models.UploadSession{
		{
			UUID:             uuid.NewString(),
			ClientUploadUUID: currentGroup,
			TusID:            "current-partial",
			Protocol:         models.UploadProtocolTus,
			Kind:             models.UploadKindPartial,
			Status:           models.UploadStatusUploading,
			Name:             "current.mp4",
			Size:             50,
			QuotaBytes:       50,
			UserID:           env.user.ID,
		},
		{
			UUID:             uuid.NewString(),
			ClientUploadUUID: currentGroup,
			TusID:            "current-final",
			Protocol:         models.UploadProtocolTus,
			Kind:             models.UploadKindFinal,
			Status:           models.UploadStatusUploaded,
			Name:             "current.mp4",
			Size:             50,
			QuotaBytes:       0,
			UserID:           env.user.ID,
		},
		{
			UUID:             uuid.NewString(),
			ClientUploadUUID: uuid.NewString(),
			TusID:            "other-upload",
			Protocol:         models.UploadProtocolTus,
			Kind:             models.UploadKindSingle,
			Status:           models.UploadStatusUploading,
			Name:             "other.mp4",
			Size:             15,
			QuotaBytes:       15,
			UserID:           env.user.ID,
		},
	}
	if err := env.db.Create(&sessions).Error; err != nil {
		t.Fatalf("create upload sessions: %v", err)
	}

	if status, err := logic.CheckStorageQuota(env.user.ID, 40, currentGroup); err != nil || status != http.StatusOK {
		t.Fatalf("expected quota check excluding current group to pass, status=%d err=%v", status, err)
	}
	if status, err := logic.CheckStorageQuota(env.user.ID, 40, ""); err == nil || status != http.StatusForbidden {
		t.Fatalf("expected quota check including current group to fail with 403, status=%d err=%v", status, err)
	}
}

func TestTusPatchHeadAndDeleteFlow(t *testing.T) {
	env := setupTusTest(t)
	rec := createTusUploadRequest(t, env.token, 11, map[string]string{"filename": "movie.mp4"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	if location == "" {
		t.Fatal("expected Location header")
	}

	srv, err := GetServer()
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	patch := httptest.NewRequest(http.MethodPatch, location, bytes.NewBufferString("hello"))
	setTusAuthHeaders(patch, env.token)
	patch.Header.Set("Content-Type", "application/offset+octet-stream")
	patch.Header.Set("Upload-Offset", "0")
	patchRec := httptest.NewRecorder()
	srv.ServeHTTP(patchRec, patch)
	if patchRec.Code != http.StatusNoContent {
		t.Fatalf("expected PATCH 204, got %d: %s", patchRec.Code, patchRec.Body.String())
	}

	head := httptest.NewRequest(http.MethodHead, location, nil)
	setTusAuthHeaders(head, env.token)
	headRec := httptest.NewRecorder()
	srv.ServeHTTP(headRec, head)
	if headRec.Code != http.StatusOK {
		t.Fatalf("expected HEAD 200, got %d: %s", headRec.Code, headRec.Body.String())
	}
	if offset := headRec.Header().Get("Upload-Offset"); offset != "5" {
		t.Fatalf("expected offset 5, got %q", offset)
	}
	if headRec.Header().Get("Upload-Expires") == "" {
		t.Fatal("expected Upload-Expires on HEAD response")
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, location, nil)
	setTusAuthHeaders(deleteReq, env.token)
	deleteRec := httptest.NewRecorder()
	srv.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("expected DELETE 204, got %d: %s", deleteRec.Code, deleteRec.Body.String())
	}

	tusID := path.Base(location)
	waitFor(t, func() bool {
		var session models.UploadSession
		if err := env.db.Unscoped().Where("tus_id = ?", tusID).First(&session).Error; err != nil {
			return false
		}
		return session.Status == models.UploadStatusCanceled && session.DeletedAt != nil && session.DeletedAt.Valid
	})
	if _, err := os.Stat(filepath.Join(config.ENV.FolderVideoUploadsPriv, "tus", tusID)); !os.IsNotExist(err) {
		t.Fatalf("expected tus data file to be removed, stat err=%v", err)
	}
}

func TestExpirationResponseWriterPreservesResponseControllerDeadlines(t *testing.T) {
	inner := newDeadlineResponseWriter()
	wrapped := &expirationResponseWriter{ResponseWriter: inner}
	controller := http.NewResponseController(wrapped)

	if err := controller.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if err := controller.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set write deadline: %v", err)
	}
	if !inner.readDeadlineSet {
		t.Fatal("expected read deadline to be forwarded to wrapped response writer")
	}
	if !inner.writeDeadlineSet {
		t.Fatal("expected write deadline to be forwarded to wrapped response writer")
	}
}

func TestTusInterruptedPatchReportsAcceptedOffset(t *testing.T) {
	env := setupTusTest(t)
	rec := createTusUploadRequest(t, env.token, 10, map[string]string{"filename": "movie.mp4"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	if location == "" {
		t.Fatal("expected Location header")
	}

	srv, err := GetServer()
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	patch := httptest.NewRequest(http.MethodPatch, location, &unexpectedEOFReadCloser{data: []byte("hello")})
	setTusAuthHeaders(patch, env.token)
	patch.ContentLength = 10
	patch.Header.Set("Content-Length", "10")
	patch.Header.Set("Content-Type", "application/offset+octet-stream")
	patch.Header.Set("Upload-Offset", "0")
	patchRec := httptest.NewRecorder()
	srv.ServeHTTP(patchRec, patch)
	if patchRec.Code != http.StatusBadRequest {
		t.Fatalf("expected interrupted PATCH 400, got %d: %s", patchRec.Code, patchRec.Body.String())
	}
	if !strings.Contains(patchRec.Body.String(), "ERR_UNEXPECTED_EOF") {
		t.Fatalf("expected unexpected EOF response, got %q", patchRec.Body.String())
	}

	head := httptest.NewRequest(http.MethodHead, location, nil)
	setTusAuthHeaders(head, env.token)
	headRec := httptest.NewRecorder()
	srv.ServeHTTP(headRec, head)
	if headRec.Code != http.StatusOK {
		t.Fatalf("expected HEAD 200, got %d: %s", headRec.Code, headRec.Body.String())
	}
	if offset := headRec.Header().Get("Upload-Offset"); offset != "5" {
		t.Fatalf("expected resumable HEAD offset 5, got %q", offset)
	}
}

func TestFinalizeRejectsPartialUploadsAndReturnsExistingLink(t *testing.T) {
	env := setupTusTest(t)
	partial := models.UploadSession{
		UUID:             uuid.NewString(),
		ClientUploadUUID: uuid.NewString(),
		TusID:            "partial-upload",
		Protocol:         models.UploadProtocolTus,
		Kind:             models.UploadKindPartial,
		Status:           models.UploadStatusUploaded,
		Name:             "movie.mp4",
		Size:             10,
		UserID:           env.user.ID,
	}
	if err := env.db.Create(&partial).Error; err != nil {
		t.Fatalf("create partial session: %v", err)
	}
	if status, link, err := Finalize(partial.TusID, env.user.ID); err == nil || link != nil || status != http.StatusBadRequest {
		t.Fatalf("expected partial finalize 400, status=%d link=%v err=%v", status, link, err)
	}

	file := models.File{
		UUID:   uuid.NewString(),
		Size:   10,
		UserID: env.user.ID,
	}
	if err := env.db.Create(&file).Error; err != nil {
		t.Fatalf("create file: %v", err)
	}
	link := models.Link{
		UUID:   uuid.NewString(),
		Name:   "movie.mp4",
		FileID: file.ID,
		UserID: env.user.ID,
	}
	if err := env.db.Create(&link).Error; err != nil {
		t.Fatalf("create link: %v", err)
	}
	done := models.UploadSession{
		UUID:             uuid.NewString(),
		ClientUploadUUID: uuid.NewString(),
		TusID:            "done-upload",
		Protocol:         models.UploadProtocolTus,
		Kind:             models.UploadKindSingle,
		Status:           models.UploadStatusDone,
		Name:             "movie.mp4",
		Size:             10,
		UserID:           env.user.ID,
		FileID:           file.ID,
		LinkID:           link.ID,
	}
	if err := env.db.Create(&done).Error; err != nil {
		t.Fatalf("create done session: %v", err)
	}

	status, got, err := Finalize(done.TusID, env.user.ID)
	if err != nil || status != http.StatusOK {
		t.Fatalf("expected idempotent finalize success, status=%d err=%v", status, err)
	}
	if got == nil || got.ID != link.ID {
		t.Fatalf("expected link %d, got %#v", link.ID, got)
	}
}

func TestFinalizeRejectsConcurrentImport(t *testing.T) {
	env := setupTusTest(t)
	storagePath := filepath.Join(env.root, "importing-upload")
	if err := os.WriteFile(storagePath, []byte("abc"), 0664); err != nil {
		t.Fatalf("write storage file: %v", err)
	}
	session := models.UploadSession{
		UUID:             uuid.NewString(),
		ClientUploadUUID: uuid.NewString(),
		TusID:            "importing-upload",
		Protocol:         models.UploadProtocolTus,
		Kind:             models.UploadKindSingle,
		Status:           models.UploadStatusImporting,
		Name:             "movie.mp4",
		Size:             3,
		StoragePath:      storagePath,
		UserID:           env.user.ID,
	}
	if err := env.db.Create(&session).Error; err != nil {
		t.Fatalf("create importing session: %v", err)
	}

	status, link, err := Finalize(session.TusID, env.user.ID)
	if err == nil || link != nil || status != http.StatusConflict {
		t.Fatalf("expected concurrent finalize 409, status=%d link=%v err=%v", status, link, err)
	}
}

func createTusUploadRequest(t *testing.T, token string, size int64, metadata map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	srv, err := GetServer()
	if err != nil {
		t.Fatalf("get server: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/uploads", nil)
	setTusAuthHeaders(req, token)
	req.Header.Set("Upload-Length", strconv.FormatInt(size, 10))
	req.Header.Set("Upload-Metadata", tusMetadata(metadata))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func setTusAuthHeaders(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Tus-Resumable", "1.0.0")
}

func tusMetadata(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := base64.StdEncoding.EncodeToString([]byte(values[key]))
		parts = append(parts, key+" "+value)
	}
	return strings.Join(parts, ",")
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}

func waitForTusStoragePath(t *testing.T, env tusTestEnv, tusID string) {
	t.Helper()
	waitFor(t, func() bool {
		var session models.UploadSession
		if err := env.db.Where("tus_id = ?", tusID).First(&session).Error; err != nil {
			return false
		}
		return session.StoragePath != "" && session.InfoPath != ""
	})
}

type deadlineResponseWriter struct {
	header           http.Header
	body             bytes.Buffer
	status           int
	readDeadlineSet  bool
	writeDeadlineSet bool
}

func newDeadlineResponseWriter() *deadlineResponseWriter {
	return &deadlineResponseWriter{header: http.Header{}}
}

func (w *deadlineResponseWriter) Header() http.Header {
	return w.header
}

func (w *deadlineResponseWriter) Write(data []byte) (int, error) {
	return w.body.Write(data)
}

func (w *deadlineResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
}

func (w *deadlineResponseWriter) SetReadDeadline(time.Time) error {
	w.readDeadlineSet = true
	return nil
}

func (w *deadlineResponseWriter) SetWriteDeadline(time.Time) error {
	w.writeDeadlineSet = true
	return nil
}

type unexpectedEOFReadCloser struct {
	data []byte
	read bool
}

func (r *unexpectedEOFReadCloser) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	n := copy(p, r.data)
	return n, io.ErrUnexpectedEOF
}

func (r *unexpectedEOFReadCloser) Close() error {
	return nil
}
