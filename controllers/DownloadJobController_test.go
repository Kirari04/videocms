package controllers

import (
	"bytes"
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/middlewares"
	"ch/kirari04/videocms/models"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	downloadJobTestLinkUUID   = "550e8400-e29b-41d4-a716-446655440000"
	downloadJobTestAudioUUID  = "550e8400-e29b-41d4-a716-446655440001"
	downloadJobTestAudioUUID2 = "550e8400-e29b-41d4-a716-446655440002"
)

func TestCreateDownloadJobDeduplicatesActiveSelection(t *testing.T) {
	h, link, _ := downloadJobTestHandlers(t, 20)
	body := []byte(`{
		"quality":"720p",
		"container":"mkv",
		"audioUUIDs":["` + downloadJobTestAudioUUID + `"],
		"subtitleUUIDs":[]
	}`)

	first := callCreateDownloadJob(t, h, link, body)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d, want %d: %s", first.Code, http.StatusAccepted, first.Body.String())
	}
	var firstResponse downloadJobResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstResponse); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if firstResponse.Status != models.DownloadJobStatusQueued || firstResponse.Reused {
		t.Fatalf("first response = %#v", firstResponse)
	}

	second := callCreateDownloadJob(t, h, link, body)
	if second.Code != http.StatusAccepted {
		t.Fatalf("second status = %d, want %d: %s", second.Code, http.StatusAccepted, second.Body.String())
	}
	var secondResponse downloadJobResponse
	if err := json.Unmarshal(second.Body.Bytes(), &secondResponse); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if !secondResponse.Reused || secondResponse.ID != firstResponse.ID {
		t.Fatalf("deduplicated response = %#v, first ID = %s", secondResponse, firstResponse.ID)
	}

	var count int64
	if err := h.Deps.DB.Model(&models.DownloadJob{}).Count(&count).Error; err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if count != 1 {
		t.Fatalf("job count = %d, want 1", count)
	}
}

func TestCreateDownloadJobHonorsBoundedQueue(t *testing.T) {
	h, link, _ := downloadJobTestHandlers(t, 1)
	if err := h.Deps.DB.Create(&models.DownloadJob{
		UUID:          "550e8400-e29b-41d4-a716-446655440010",
		LinkID:        link.ID,
		LinkUUID:      link.UUID,
		FileID:        link.FileID,
		UserID:        link.UserID,
		SelectionHash: "different",
		Status:        models.DownloadJobStatusQueued,
	}).Error; err != nil {
		t.Fatalf("create queued job: %v", err)
	}

	body := []byte(`{
		"quality":"720p",
		"container":"mkv",
		"audioUUIDs":["` + downloadJobTestAudioUUID + `"],
		"subtitleUUIDs":[]
	}`)
	recorder := callCreateDownloadJob(t, h, link, body)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusTooManyRequests, recorder.Body.String())
	}
	if recorder.Header().Get("Retry-After") != "30" {
		t.Fatalf("Retry-After = %q, want 30", recorder.Header().Get("Retry-After"))
	}
}

func TestCreateDownloadJobDeduplicatesBeforeQueueLimit(t *testing.T) {
	h, link, _ := downloadJobTestHandlers(t, 1)
	body := []byte(`{
		"quality":"720p",
		"container":"mkv",
		"audioUUIDs":["` + downloadJobTestAudioUUID + `"],
		"subtitleUUIDs":[]
	}`)
	first := callCreateDownloadJob(t, h, link, body)
	second := callCreateDownloadJob(t, h, link, body)
	if first.Code != http.StatusAccepted || second.Code != http.StatusAccepted {
		t.Fatalf("statuses = %d/%d, want accepted deduplication", first.Code, second.Code)
	}
	var firstResponse, secondResponse downloadJobResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstResponse); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondResponse); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if !secondResponse.Reused || firstResponse.ID != secondResponse.ID {
		t.Fatalf("second response = %#v, first = %#v", secondResponse, firstResponse)
	}
}

func TestCreateDownloadJobReusesReadyArtifactButNotFailedWork(t *testing.T) {
	h, link, root := downloadJobTestHandlers(t, 20)
	body := []byte(`{
		"quality":"720p",
		"container":"mkv",
		"audioUUIDs":["` + downloadJobTestAudioUUID + `"],
		"subtitleUUIDs":[]
	}`)

	first := callCreateDownloadJob(t, h, link, body)
	var firstResponse downloadJobResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstResponse); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	var firstJob models.DownloadJob
	if err := h.Deps.DB.Where("uuid = ?", firstResponse.ID).First(&firstJob).Error; err != nil {
		t.Fatalf("load first job: %v", err)
	}
	outputDir := filepath.Join(root, "download-jobs")
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}
	outputPath := filepath.Join(outputDir, firstJob.UUID+".mkv")
	if err := os.WriteFile(outputPath, []byte("ready"), 0o600); err != nil {
		t.Fatalf("write output: %v", err)
	}
	now := time.Now()
	expires := now.Add(time.Hour)
	if err := h.Deps.DB.Model(&firstJob).Updates(map[string]interface{}{
		"status":      models.DownloadJobStatusReady,
		"output_path": outputPath,
		"output_size": 5,
		"ready_at":    &now,
		"expires_at":  &expires,
	}).Error; err != nil {
		t.Fatalf("mark first job ready: %v", err)
	}

	readyReuse := callCreateDownloadJob(t, h, link, body)
	if readyReuse.Code != http.StatusOK {
		t.Fatalf("ready reuse status = %d, want %d", readyReuse.Code, http.StatusOK)
	}
	var readyResponse downloadJobResponse
	if err := json.Unmarshal(readyReuse.Body.Bytes(), &readyResponse); err != nil {
		t.Fatalf("decode ready reuse: %v", err)
	}
	if !readyResponse.Reused || readyResponse.ID != firstResponse.ID || readyResponse.File == nil {
		t.Fatalf("ready reuse response = %#v", readyResponse)
	}

	if err := h.Deps.DB.Model(&firstJob).Updates(map[string]interface{}{
		"status":        models.DownloadJobStatusFailed,
		"output_path":   "",
		"output_size":   0,
		"expires_at":    nil,
		"error_code":    "test_failure",
		"error_message": "Test failure.",
	}).Error; err != nil {
		t.Fatalf("mark first job failed: %v", err)
	}
	failedRetry := callCreateDownloadJob(t, h, link, body)
	var retryResponse downloadJobResponse
	if err := json.Unmarshal(failedRetry.Body.Bytes(), &retryResponse); err != nil {
		t.Fatalf("decode failed retry: %v", err)
	}
	if failedRetry.Code != http.StatusAccepted || retryResponse.ID == firstResponse.ID || retryResponse.Reused {
		t.Fatalf("failed retry response = %#v, status=%d", retryResponse, failedRetry.Code)
	}
}

func TestCreateDownloadJobTrackOrderChangesSelectionHash(t *testing.T) {
	h, link, _ := downloadJobTestHandlers(t, 20)
	first := callCreateDownloadJob(t, h, link, []byte(`{
		"quality":"720p",
		"container":"mkv",
		"audioUUIDs":["`+downloadJobTestAudioUUID+`","`+downloadJobTestAudioUUID2+`"],
		"subtitleUUIDs":[]
	}`))
	second := callCreateDownloadJob(t, h, link, []byte(`{
		"quality":"720p",
		"container":"mkv",
		"audioUUIDs":["`+downloadJobTestAudioUUID2+`","`+downloadJobTestAudioUUID+`"],
		"subtitleUUIDs":[]
	}`))

	var firstResponse, secondResponse downloadJobResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstResponse); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondResponse); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if firstResponse.ID == secondResponse.ID || secondResponse.Reused {
		t.Fatalf("ordered selections unexpectedly deduplicated: first=%s second=%#v", firstResponse.ID, secondResponse)
	}
}

func TestCreateDownloadJobQueueLimitDoesNotCountPreparingWorkers(t *testing.T) {
	h, link, _ := downloadJobTestHandlers(t, 1)
	if err := h.Deps.DB.Create(&models.DownloadJob{
		UUID:          "550e8400-e29b-41d4-a716-446655440011",
		LinkID:        link.ID,
		LinkUUID:      link.UUID,
		FileID:        link.FileID,
		UserID:        link.UserID,
		SelectionHash: "different",
		Status:        models.DownloadJobStatusPreparing,
	}).Error; err != nil {
		t.Fatalf("create preparing job: %v", err)
	}

	body := []byte(`{
		"quality":"720p",
		"container":"mkv",
		"audioUUIDs":["` + downloadJobTestAudioUUID + `"],
		"subtitleUUIDs":[]
	}`)
	recorder := callCreateDownloadJob(t, h, link, body)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}
}

func TestDownloadPreparedFileCountsFullAndRangeBytesAsDownloadTraffic(t *testing.T) {
	h, link, root := downloadJobTestHandlers(t, 20)
	outputDir := filepath.Join(root, "download-jobs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}
	outputPath := filepath.Join(outputDir, "job.mkv")
	if err := os.WriteFile(outputPath, []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}
	now := time.Now()
	expires := now.Add(time.Hour)
	job := models.DownloadJob{
		UUID:          "550e8400-e29b-41d4-a716-446655440020",
		LinkID:        link.ID,
		LinkUUID:      link.UUID,
		FileID:        link.FileID,
		UserID:        link.UserID,
		QualityID:     link.File.Qualitys[0].ID,
		QualityName:   "720p",
		Container:     "mkv",
		AudioUUIDs:    "[]",
		SubtitleUUIDs: "[]",
		Status:        models.DownloadJobStatusReady,
		Progress:      1,
		OutputPath:    outputPath,
		OutputName:    "video[720p].mkv",
		OutputSize:    10,
		ReadyAt:       &now,
		ExpiresAt:     &expires,
	}
	if err := h.Deps.DB.Create(&job).Error; err != nil {
		t.Fatalf("create ready job: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/prepared", nil)
	req.Header.Set("Range", "bytes=2-5")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("UUID", "JOBUUID")
	c.SetParamValues(link.UUID, job.UUID)
	c.Set(middlewares.MediaClaimsContextKey, &auth.MediaClaims{
		LinkUUID: link.UUID,
		FileID:   link.FileID,
		UserID:   link.UserID,
	})

	if err := h.DownloadPreparedFile(c); err != nil {
		t.Fatalf("DownloadPreparedFile() error = %v", err)
	}
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusPartialContent)
	}
	if rec.Body.String() != "2345" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "2345")
	}

	var traffic models.TrafficLog
	if err := h.Deps.DB.First(&traffic).Error; err != nil {
		t.Fatalf("load traffic: %v", err)
	}
	if traffic.Source != models.TrafficSourceDownload || traffic.Bytes != 4 {
		t.Fatalf("traffic = %#v, want download/4 bytes", traffic)
	}
	if traffic.UserID != link.UserID {
		t.Fatalf("traffic user = %d, want link owner %d", traffic.UserID, link.UserID)
	}

	fullReq := httptest.NewRequest(http.MethodGet, "/prepared", nil)
	fullRec := httptest.NewRecorder()
	fullContext := e.NewContext(fullReq, fullRec)
	fullContext.SetParamNames("UUID", "JOBUUID")
	fullContext.SetParamValues(link.UUID, job.UUID)
	fullContext.Set(middlewares.MediaClaimsContextKey, &auth.MediaClaims{
		LinkUUID: link.UUID,
		FileID:   link.FileID,
		UserID:   link.UserID,
	})
	if err := h.DownloadPreparedFile(fullContext); err != nil {
		t.Fatalf("full DownloadPreparedFile() error = %v", err)
	}
	if fullRec.Code != http.StatusOK || fullRec.Body.String() != "0123456789" {
		t.Fatalf("full response = status %d body %q", fullRec.Code, fullRec.Body.String())
	}
	var delivered uint64
	if err := h.Deps.DB.Model(&models.TrafficLog{}).Select("COALESCE(SUM(bytes), 0)").Scan(&delivered).Error; err != nil {
		t.Fatalf("sum traffic: %v", err)
	}
	if delivered != 14 {
		t.Fatalf("delivered bytes = %d, want 14 (4 range + 10 full)", delivered)
	}

	disconnect := &disconnectingResponseWriter{
		header:    http.Header{},
		remaining: 3,
	}
	disconnectReq := httptest.NewRequest(http.MethodGet, "/prepared", nil)
	disconnectContext := e.NewContext(disconnectReq, disconnect)
	disconnectContext.SetParamNames("UUID", "JOBUUID")
	disconnectContext.SetParamValues(link.UUID, job.UUID)
	disconnectContext.Set(middlewares.MediaClaimsContextKey, &auth.MediaClaims{
		LinkUUID: link.UUID,
		FileID:   link.FileID,
		UserID:   link.UserID,
	})
	if err := h.DownloadPreparedFile(disconnectContext); err != nil {
		t.Fatalf("disconnected DownloadPreparedFile() error = %v", err)
	}
	if err := h.Deps.DB.Model(&models.TrafficLog{}).Select("COALESCE(SUM(bytes), 0)").Scan(&delivered).Error; err != nil {
		t.Fatalf("sum disconnected traffic: %v", err)
	}
	if delivered != 17 {
		t.Fatalf("delivered bytes after disconnect = %d, want 17 (only 3 additional bytes)", delivered)
	}
}

type disconnectingResponseWriter struct {
	header    http.Header
	status    int
	remaining int
}

func (w *disconnectingResponseWriter) Header() http.Header {
	return w.header
}

func (w *disconnectingResponseWriter) WriteHeader(status int) {
	w.status = status
}

func (w *disconnectingResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.remaining <= 0 {
		return 0, errors.New("client disconnected")
	}
	if len(data) <= w.remaining {
		w.remaining -= len(data)
		return len(data), nil
	}
	n := w.remaining
	w.remaining = 0
	return n, errors.New("client disconnected")
}

func callCreateDownloadJob(t *testing.T, h *Handlers, link *models.Link, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/download-jobs", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("UUID")
	c.SetParamValues(link.UUID)
	c.Set(middlewares.MediaClaimsContextKey, &auth.MediaClaims{
		LinkUUID: link.UUID,
		FileID:   link.FileID,
		UserID:   link.UserID,
	})
	if err := h.CreateDownloadJob(c); err != nil {
		t.Fatalf("CreateDownloadJob() error = %v", err)
	}
	return rec
}

func downloadJobTestHandlers(t *testing.T, maxQueued int64) (*Handlers, *models.Link, string) {
	t.Helper()
	db, err := gorm.Open(
		sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"),
		&gorm.Config{},
	)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.File{},
		&models.Link{},
		&models.Quality{},
		&models.Audio{},
		&models.Subtitle{},
		&models.DownloadJob{},
		&models.TrafficLog{},
	); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	root := t.TempDir()
	enabled := true
	deps := &app.Deps{
		DB: db,
		Snapshots: app.NewSnapshotStore(app.Snapshot{Config: config.Config{
			FolderVideoQualitysPriv:           filepath.Join(root, "quality"),
			FolderVideoQualitysPub:            "/videos/qualitys",
			FolderVideoUploadsPriv:            root,
			DownloadEnabled:                   &enabled,
			MaxParallelDownloadPreparations:   1,
			MaxQueuedDownloadPreparations:     maxQueued,
			DownloadPreparationRetentionHours: 6,
		}}),
		RequestGate: app.NewRequestGate(),
	}
	file := models.File{
		UUID:     "550e8400-e29b-41d4-a716-446655440100",
		Duration: 3600,
		UserID:   99,
	}
	if err := db.Create(&file).Error; err != nil {
		t.Fatalf("create file: %v", err)
	}
	quality := models.Quality{
		FileID:     file.ID,
		Name:       "720p",
		Type:       "hls",
		Ready:      true,
		Path:       filepath.Join(root, "quality", "720p"),
		OutputFile: "out.m3u8",
	}
	audio := models.Audio{
		FileID:     file.ID,
		UUID:       downloadJobTestAudioUUID,
		Name:       "English",
		Lang:       "eng",
		Codec:      "aac",
		Ready:      true,
		Path:       filepath.Join(root, "audio"),
		OutputFile: "audio.m3u8",
	}
	audio2 := models.Audio{
		FileID:     file.ID,
		UUID:       downloadJobTestAudioUUID2,
		Name:       "German",
		Lang:       "deu",
		Codec:      "aac",
		Ready:      true,
		Path:       filepath.Join(root, "audio-2"),
		OutputFile: "audio.m3u8",
	}
	if err := db.Create(&quality).Error; err != nil {
		t.Fatalf("create quality: %v", err)
	}
	if err := db.Create(&audio).Error; err != nil {
		t.Fatalf("create audio: %v", err)
	}
	if err := db.Create(&audio2).Error; err != nil {
		t.Fatalf("create second audio: %v", err)
	}
	link := models.Link{
		UUID:   downloadJobTestLinkUUID,
		Name:   "Test Video",
		FileID: file.ID,
		UserID: 7,
	}
	if err := db.Create(&link).Error; err != nil {
		t.Fatalf("create link: %v", err)
	}
	if err := db.
		Preload("File").
		Preload("File.Qualitys").
		Preload("File.Audios").
		Preload("File.Subtitles").
		First(&link, link.ID).Error; err != nil {
		t.Fatalf("reload link: %v", err)
	}
	logicSvc := logic.NewService(deps)
	return NewHandlers(deps, auth.NewService(deps), logicSvc, nil, nil), &link, root
}
