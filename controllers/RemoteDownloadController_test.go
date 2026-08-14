package controllers

import (
	"bytes"
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/background"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/labstack/echo/v4"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCreateRemoteDownloadReplaysIdempotentBatch(t *testing.T) {
	h, user := remoteDownloadTestHandlers(t)
	body := []byte(`{"urls":["https://example.test/one.mp4","https://example.test/two.mp4"],"parentFolderID":0}`)
	first := callCreateRemoteDownload(t, h, user.ID, "request-1", body)
	second := callCreateRemoteDownload(t, h, user.ID, "request-1", body)
	if first.Code != http.StatusAccepted || second.Code != http.StatusAccepted {
		t.Fatalf("statuses = %d/%d, bodies = %q / %q", first.Code, second.Code, first.Body.String(), second.Body.String())
	}
	var firstRows, secondRows []models.RemoteDownload
	if err := json.Unmarshal(first.Body.Bytes(), &firstRows); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondRows); err != nil {
		t.Fatal(err)
	}
	if len(firstRows) != 2 || len(secondRows) != 2 || firstRows[0].ID != secondRows[0].ID || firstRows[1].BackgroundJobID != secondRows[1].BackgroundJobID {
		t.Fatalf("request was not replayed: first=%#v second=%#v", firstRows, secondRows)
	}
	var rows, jobs int64
	if err := h.Deps.DB.Model(&models.RemoteDownload{}).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.Deps.DB.Model(&background.Job{}).Count(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 2 || jobs != 2 {
		t.Fatalf("expected 2 domain rows and jobs, got rows=%d jobs=%d", rows, jobs)
	}
}

func TestCreateRemoteDownloadRejectsChangedIdempotentRequest(t *testing.T) {
	h, user := remoteDownloadTestHandlers(t)
	first := callCreateRemoteDownload(t, h, user.ID, "request-2", []byte(`{"urls":["https://example.test/one.mp4"]}`))
	changed := callCreateRemoteDownload(t, h, user.ID, "request-2", []byte(`{"urls":["https://example.test/different.mp4"]}`))
	if first.Code != http.StatusAccepted || changed.Code != http.StatusConflict {
		t.Fatalf("statuses = %d/%d, changed body=%q", first.Code, changed.Code, changed.Body.String())
	}
}

func TestRemoteDownloadLegacyAndV2KeepTheirResponseStatuses(t *testing.T) {
	h, user := remoteDownloadTestHandlers(t)
	body := []byte(`{"urls":["https://example.test/contract.mp4"]}`)

	v2 := callCreateRemoteDownloadWith(t, h, user.ID, "contract-v2", body, h.CreateRemoteDownloadV2)
	legacy := callCreateRemoteDownloadWith(t, h, user.ID, "contract-legacy", body, h.CreateRemoteDownloadLegacy)

	if v2.Code != http.StatusAccepted {
		t.Fatalf("v2 status = %d, want %d: %s", v2.Code, http.StatusAccepted, v2.Body.String())
	}
	if legacy.Code != http.StatusCreated {
		t.Fatalf("legacy status = %d, want %d: %s", legacy.Code, http.StatusCreated, legacy.Body.String())
	}
	for name, recorder := range map[string]*httptest.ResponseRecorder{"v2": v2, "legacy": legacy} {
		var rows []models.RemoteDownload
		if err := json.Unmarshal(recorder.Body.Bytes(), &rows); err != nil {
			t.Fatalf("%s response: %v", name, err)
		}
		if len(rows) != 1 || rows[0].BackgroundJobID == "" {
			t.Fatalf("%s response did not expose its durable job: %#v", name, rows)
		}
	}
}

func TestRemoteDownloadCancelLegacyAndV2KeepStatusesAndProjection(t *testing.T) {
	h, user := remoteDownloadTestHandlers(t)
	body := []byte(`{"urls":["https://example.test/cancel.mp4"]}`)

	create := func(key string) models.RemoteDownload {
		recorder := callCreateRemoteDownloadWith(t, h, user.ID, key, body, h.CreateRemoteDownloadV2)
		var rows []models.RemoteDownload
		if err := json.Unmarshal(recorder.Body.Bytes(), &rows); err != nil || len(rows) != 1 {
			t.Fatalf("create %q: rows=%#v err=%v", key, rows, err)
		}
		return rows[0]
	}

	v2Download := create("cancel-v2")
	legacyDownload := create("cancel-legacy")
	v2 := callCancelRemoteDownload(t, h, user.ID, v2Download.ID, h.CancelRemoteDownload)
	legacy := callCancelRemoteDownload(t, h, user.ID, legacyDownload.ID, h.CancelRemoteDownloadLegacy)
	if v2.Code != http.StatusAccepted || legacy.Code != http.StatusOK {
		t.Fatalf("cancel statuses = v2:%d legacy:%d", v2.Code, legacy.Code)
	}
	for _, id := range []uint{v2Download.ID, legacyDownload.ID} {
		var download models.RemoteDownload
		if err := h.Deps.DB.First(&download, id).Error; err != nil {
			t.Fatal(err)
		}
		if download.Status != models.RemoteDownloadStatusCanceled || download.CanceledAt == nil || download.FinishedAt == nil {
			t.Fatalf("download %d cancellation projection = %#v", id, download)
		}
	}
}

func remoteDownloadTestHandlers(t *testing.T) (*Handlers, *models.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "remote.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Folder{}, &models.RemoteDownload{}); err != nil {
		t.Fatal(err)
	}
	if err := background.Migrate(db); err != nil {
		t.Fatal(err)
	}
	enabled := true
	deps := &app.Deps{
		DB: db, RequestGate: app.NewRequestGate(),
		Snapshots: app.NewSnapshotStore(app.Snapshot{Config: config.Config{RemoteDownloadEnabled: &enabled, MaxParallelDownloads: 2}}),
	}
	deps.Background = background.New(db, background.Options{})
	user := &models.User{Username: "remote-user", Settings: models.UserSettings{MaxRemoteDownloads: 10, RemoteDownloadEnabled: &enabled}}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	logicService := logic.NewService(deps)
	return NewHandlers(deps, auth.NewService(deps), logicService, nil, nil), user
}

func callCreateRemoteDownload(t *testing.T, h *Handlers, userID uint, key string, body []byte) *httptest.ResponseRecorder {
	return callCreateRemoteDownloadWith(t, h, userID, key, body, h.CreateRemoteDownload)
}

func callCreateRemoteDownloadWith(t *testing.T, h *Handlers, userID uint, key string, body []byte, handler echo.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/remote/download", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set("Idempotency-Key", key)
	recorder := httptest.NewRecorder()
	c := e.NewContext(req, recorder)
	c.Set("UserID", userID)
	if err := handler(c); err != nil {
		t.Fatalf("create remote download: %v", err)
	}
	return recorder
}

func callCancelRemoteDownload(t *testing.T, h *Handlers, userID, downloadID uint, handler echo.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	recorder := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodPost, "/remote/download/"+strconv.FormatUint(uint64(downloadID), 10)+"/cancel", nil), recorder)
	c.SetParamNames("id")
	c.SetParamValues(strconv.FormatUint(uint64(downloadID), 10))
	c.Set("UserID", userID)
	c.Set("Username", "remote-user")
	if err := handler(c); err != nil {
		t.Fatalf("cancel remote download: %v", err)
	}
	return recorder
}
