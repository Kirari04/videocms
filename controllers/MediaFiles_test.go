package controllers

import (
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/middlewares"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
)

const testLinkUUID = "550e8400-e29b-41d4-a716-446655440000"
const testAudioUUID = "550e8400-e29b-41d4-a716-446655440001"
const testSubtitleUUID = "550e8400-e29b-41d4-a716-446655440002"

func TestGetVideoDataUsesMediaClaims(t *testing.T) {
	restore := setPrivateMediaRoot(t.TempDir())
	defer restore()
	mustWriteEmptyFile(t, filepath.Join(config.ENV.FolderVideoQualitysPriv, "file-uuid", "720p", "out0.ts"))

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

	if err := GetVideoData(c); err != nil {
		t.Fatalf("GetVideoData() error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestGetVideoDataRejectsMissingMediaClaims(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/videos/qualitys/"+testLinkUUID+"/720p/out0.ts", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("UUID", "QUALITY", "FILE")
	c.SetParamValues(testLinkUUID, "720p", "out0.ts")

	if err := GetVideoData(c); err != nil {
		t.Fatalf("GetVideoData() error = %v", err)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestGetAudioDataUsesMediaClaims(t *testing.T) {
	restore := setPrivateMediaRoot(t.TempDir())
	defer restore()
	mustWriteEmptyFile(t, filepath.Join(config.ENV.FolderVideoQualitysPriv, "file-uuid", testAudioUUID, "audio0.ts"))

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

	if err := GetAudioData(c); err != nil {
		t.Fatalf("GetAudioData() error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestGetSubtitleDataUsesMediaClaims(t *testing.T) {
	restore := setPrivateMediaRoot(t.TempDir())
	defer restore()
	mustWriteEmptyFile(t, filepath.Join(config.ENV.FolderVideoQualitysPriv, "file-uuid", testSubtitleUUID, "out.vtt"))

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

	if err := GetSubtitleData(c); err != nil {
		t.Fatalf("GetSubtitleData() error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestDownloadVideoHonorsDownloadEnabledBeforeDatabaseLookup(t *testing.T) {
	previous := config.ENV.DownloadEnabled
	disabled := false
	config.ENV.DownloadEnabled = &disabled
	defer func() {
		config.ENV.DownloadEnabled = previous
	}()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/videos/qualitys/"+testLinkUUID+"/720p/download/video.mkv", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("UUID", "QUALITY")
	c.SetParamValues(testLinkUUID, "720p")

	if err := DownloadVideoController(c); err != nil {
		t.Fatalf("DownloadVideoController() error = %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func setPrivateMediaRoot(root string) func() {
	previous := config.ENV.FolderVideoQualitysPriv
	config.ENV.FolderVideoQualitysPriv = root
	return func() {
		config.ENV.FolderVideoQualitysPriv = previous
	}
}

func mustWriteEmptyFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
