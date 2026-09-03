package controllers

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/mediacache"
	"ch/kirari04/videocms/middlewares"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"
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

const testLinkUUID = "550e8400-e29b-41d4-a716-446655440000"
const testAudioUUID = "550e8400-e29b-41d4-a716-446655440001"
const testSubtitleUUID = "550e8400-e29b-41d4-a716-446655440002"

func TestGetVideoDataUsesMediaClaims(t *testing.T) {
	h := mediaTestHandlers(t, t.TempDir(), true)
	mustWriteEmptyFile(t, filepath.Join(h.Config().FolderVideoQualitysPriv, "file-uuid", "720p", "out0.ts"))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/videos/qualitys/"+testLinkUUID+"/720p/out0.ts", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("UUID", "QUALITY", "FILE")
	c.SetParamValues(testLinkUUID, "720p", "out0.ts")
	c.Set(middlewares.MediaClaimsContextKey, &auth.MediaClaims{
		LinkUUID:   testLinkUUID,
		FileUUID:   "file-uuid",
		UserID:     1,
		FileID:     2,
		QualityIDs: map[string]uint{"720p": 3},
	})

	if err := h.GetVideoData(c); err != nil {
		t.Fatalf("GetVideoData() error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestGetVideoDataUsesClaimedNamedStore(t *testing.T) {
	defaultRoot := t.TempDir()
	archiveRoot := t.TempDir()
	h := mediaTestHandlersWithStores(t, defaultRoot, map[string]string{
		"local":   defaultRoot,
		"archive": archiveRoot,
	}, true)
	mustWriteFile(t, filepath.Join(archiveRoot, "file-uuid", "720p", "out0.ts"), []byte("archive"))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/videos/qualitys/"+testLinkUUID+"/720p/out0.ts", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("UUID", "QUALITY", "FILE")
	c.SetParamValues(testLinkUUID, "720p", "out0.ts")
	c.Set(middlewares.MediaClaimsContextKey, &auth.MediaClaims{
		LinkUUID:   testLinkUUID,
		FileUUID:   "file-uuid",
		StorageID:  "archive",
		UserID:     1,
		FileID:     2,
		QualityIDs: map[string]uint{"720p": 3},
	})

	if err := h.GetVideoData(c); err != nil {
		t.Fatalf("GetVideoData() error = %v", err)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "archive" {
		t.Fatalf("response = %d %q, want archive object", rec.Code, rec.Body.String())
	}
}

func TestGetVideoDataSupportsRangesAndTracksDeliveredBytes(t *testing.T) {
	h := mediaTestHandlers(t, t.TempDir(), true)
	mediaPath := filepath.Join(h.Config().FolderVideoQualitysPriv, "file-uuid", "720p", "out0.ts")
	mustWriteFile(t, mediaPath, []byte("0123456789"))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/videos/qualitys/"+testLinkUUID+"/720p/out0.ts", nil)
	req.Header.Set("Range", "bytes=2-5")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("UUID", "QUALITY", "FILE")
	c.SetParamValues(testLinkUUID, "720p", "out0.ts")
	c.Set(middlewares.MediaClaimsContextKey, &auth.MediaClaims{
		LinkUUID:   testLinkUUID,
		FileUUID:   "file-uuid",
		UserID:     1,
		FileID:     2,
		QualityIDs: map[string]uint{"720p": 3},
	})

	if err := h.GetVideoData(c); err != nil {
		t.Fatalf("GetVideoData() error = %v", err)
	}
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusPartialContent)
	}
	if rec.Body.String() != "2345" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "2345")
	}

	var traffic models.TrafficLog
	if err := h.Deps.DB.First(&traffic).Error; err != nil {
		t.Fatalf("load traffic log: %v", err)
	}
	if traffic.Bytes != 4 || traffic.UserID != 1 || traffic.FileID != 2 || traffic.QualityID != 3 {
		t.Fatalf("traffic log = %#v, want four delivered bytes for the selected quality", traffic)
	}
	if traffic.StorageMountUUID != "local" || traffic.DeliverySource != models.TrafficDeliverySourceOrigin {
		t.Fatalf("storage attribution = mount %q source %q, want local origin", traffic.StorageMountUUID, traffic.DeliverySource)
	}
}

func TestGetVideoDataRangeFillsAndThenServesReadCache(t *testing.T) {
	cacheRoot := t.TempDir()
	originRoot := t.TempDir()
	h := mediaTestHandlersWithStores(t, cacheRoot, map[string]string{
		"local": cacheRoot, "archive": originRoot,
	}, true)
	if err := h.Deps.DB.AutoMigrate(
		&models.StorageMount{}, &models.StoragePool{}, &models.StoragePoolMount{},
		&models.StorageCacheEntry{}, &models.File{},
	); err != nil {
		t.Fatal(err)
	}
	cacheMount := models.StorageMount{UUID: "local", Name: "Local cache", Provider: models.StorageProviderLocal, Mounted: true, System: true}
	originMount := models.StorageMount{UUID: "archive", Name: "Remote origin", Provider: models.StorageProviderS3, Mounted: true}
	pool := models.StoragePool{UUID: "remote-pool", Name: "Remote pool"}
	for _, value := range []any{&cacheMount, &originMount, &pool} {
		if err := h.Deps.DB.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, membership := range []models.StoragePoolMount{
		{StoragePoolID: pool.ID, StorageMountID: originMount.ID, Role: models.StoragePoolMountPrimary},
		{StoragePoolID: pool.ID, StorageMountID: cacheMount.ID, Role: models.StoragePoolMountCache, CacheMaxBytes: 1024 * 1024},
	} {
		if err := h.Deps.DB.Create(&membership).Error; err != nil {
			t.Fatal(err)
		}
	}
	file := models.File{
		UUID: "file-uuid", StorageID: originMount.UUID, StoragePoolID: &pool.ID,
		StorageState: models.FileStorageAvailable,
	}
	if err := h.Deps.DB.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	h.Deps.MediaCache = mediacache.New(h.Deps.DB, h.Deps.Storage, nil)
	t.Cleanup(h.Deps.MediaCache.Close)
	body := []byte("0123456789")
	mustWriteFile(t, filepath.Join(originRoot, "file-uuid", "720p", "out0.ts"), body)

	request := func(rangeHeader string) *httptest.ResponseRecorder {
		t.Helper()
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/videos/qualitys/"+testLinkUUID+"/720p/out0.ts", nil)
		if rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("UUID", "QUALITY", "FILE")
		c.SetParamValues(testLinkUUID, "720p", "out0.ts")
		c.Set(middlewares.MediaClaimsContextKey, &auth.MediaClaims{
			LinkUUID: testLinkUUID, FileUUID: file.UUID, StorageID: originMount.UUID,
			StoragePoolID: pool.ID, UserID: 1, FileID: file.ID, QualityIDs: map[string]uint{"720p": 3},
		})
		if err := h.GetVideoData(c); err != nil {
			t.Fatalf("GetVideoData() error = %v", err)
		}
		return rec
	}

	first := request("bytes=2-5")
	if first.Code != http.StatusPartialContent || first.Body.String() != "2345" {
		t.Fatalf("first response = %d %q", first.Code, first.Body.String())
	}
	if status := first.Header().Get("X-VideoCMS-Cache"); status != mediacache.CacheStatusFilling {
		t.Fatalf("first cache status = %q, want %q", status, mediacache.CacheStatusFilling)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		if err := h.Deps.DB.Model(&models.StorageCacheEntry{}).Count(&count).Error; err == nil && count == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var entries int64
	if err := h.Deps.DB.Model(&models.StorageCacheEntry{}).Count(&entries).Error; err != nil || entries != 1 {
		t.Fatalf("cache entries = %d, err=%v", entries, err)
	}
	if err := os.Remove(filepath.Join(originRoot, "file-uuid", "720p", "out0.ts")); err != nil {
		t.Fatal(err)
	}
	second := request("")
	if second.Code != http.StatusOK || second.Body.String() != string(body) {
		t.Fatalf("second response = %d %q", second.Code, second.Body.String())
	}
	if status := second.Header().Get("X-VideoCMS-Cache"); status != mediacache.CacheStatusHit {
		t.Fatalf("second cache status = %q, want %q", status, mediacache.CacheStatusHit)
	}
	var traffic []models.TrafficLog
	if err := h.Deps.DB.Order("id ASC").Find(&traffic).Error; err != nil {
		t.Fatal(err)
	}
	if len(traffic) != 2 || traffic[0].DeliverySource != models.TrafficDeliverySourceOrigin || traffic[1].DeliverySource != models.TrafficDeliverySourceCache {
		t.Fatalf("traffic attribution = %#v", traffic)
	}
}

func TestGetVideoDataRejectsMissingMediaClaims(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/videos/qualitys/"+testLinkUUID+"/720p/out0.ts", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("UUID", "QUALITY", "FILE")
	c.SetParamValues(testLinkUUID, "720p", "out0.ts")

	h := mediaTestHandlers(t, t.TempDir(), true)
	if err := h.GetVideoData(c); err != nil {
		t.Fatalf("GetVideoData() error = %v", err)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestGetAudioDataUsesMediaClaims(t *testing.T) {
	h := mediaTestHandlers(t, t.TempDir(), true)
	mustWriteEmptyFile(t, filepath.Join(h.Config().FolderVideoQualitysPriv, "file-uuid", testAudioUUID, "audio0.ts"))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/videos/qualitys/"+testLinkUUID+"/"+testAudioUUID+"/audio/audio0.ts", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("UUID", "AUDIOUUID", "FILE")
	c.SetParamValues(testLinkUUID, testAudioUUID, "audio0.ts")
	c.Set(middlewares.MediaClaimsContextKey, &auth.MediaClaims{
		LinkUUID: testLinkUUID,
		FileUUID: "file-uuid",
		UserID:   1,
		FileID:   2,
		AudioIDs: map[string]uint{testAudioUUID: 4},
	})

	if err := h.GetAudioData(c); err != nil {
		t.Fatalf("GetAudioData() error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestGetSubtitleDataUsesMediaClaims(t *testing.T) {
	h := mediaTestHandlers(t, t.TempDir(), true)
	mustWriteEmptyFile(t, filepath.Join(h.Config().FolderVideoQualitysPriv, "file-uuid", testSubtitleUUID, "out.vtt"))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/videos/qualitys/"+testLinkUUID+"/"+testSubtitleUUID+"/subtitle/out.vtt", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("UUID", "SUBUUID", "FILE")
	c.SetParamValues(testLinkUUID, testSubtitleUUID, "out.vtt")
	c.Set(middlewares.MediaClaimsContextKey, &auth.MediaClaims{
		LinkUUID:      testLinkUUID,
		FileUUID:      "file-uuid",
		UserID:        1,
		FileID:        2,
		SubtitleUUIDs: []string{testSubtitleUUID},
	})

	if err := h.GetSubtitleData(c); err != nil {
		t.Fatalf("GetSubtitleData() error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestDownloadVideoHonorsDownloadEnabledBeforeDatabaseLookup(t *testing.T) {
	h := mediaTestHandlers(t, t.TempDir(), false)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/videos/qualitys/"+testLinkUUID+"/720p/1/stream/video.mp4", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("UUID", "QUALITY", "STREAM")
	c.SetParamValues(testLinkUUID, "720p", "1")

	if err := h.DownloadVideoController(c); err != nil {
		t.Fatalf("DownloadVideoController() error = %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func mediaTestHandlers(t *testing.T, root string, downloadEnabled bool) *Handlers {
	return mediaTestHandlersWithStores(t, root, map[string]string{"local": root}, downloadEnabled)
}

func mediaTestHandlersWithStores(t *testing.T, root string, roots map[string]string, downloadEnabled bool) *Handlers {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(&models.TrafficLog{}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	stores := make(map[string]storage.Store, len(roots))
	for id, storeRoot := range roots {
		localStore, err := storage.NewLocalStore(storeRoot)
		if err != nil {
			t.Fatalf("create %s store: %v", id, err)
		}
		stores[id] = localStore
	}
	storageService, err := storage.NewService(
		"local",
		storage.LegacyMediaLayout{},
		stores,
	)
	if err != nil {
		t.Fatalf("create storage service: %v", err)
	}
	t.Cleanup(func() { _ = storageService.Close() })
	deps := &app.Deps{
		DB: db,
		Snapshots: app.NewSnapshotStore(app.Snapshot{Config: config.Config{
			FolderVideoQualitysPriv: root,
			FolderVideoQualitysPub:  "/videos/qualitys",
			DownloadEnabled:         &downloadEnabled,
		}}),
		RequestGate: app.NewRequestGate(),
		Storage:     storageService,
	}
	logicSvc := logic.NewService(deps)
	authSvc := auth.NewService(deps)
	return NewHandlers(deps, authSvc, logicSvc, nil, nil)
}

func mustWriteEmptyFile(t *testing.T, path string) {
	t.Helper()
	mustWriteFile(t, path, nil)
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
