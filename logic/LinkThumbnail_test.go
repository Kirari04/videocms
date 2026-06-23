package logic

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestResolvedThumbnailURLPrefersLinkThumbnail(t *testing.T) {
	restore := setThumbnailPublicRoot("/videos/qualitys")
	defer restore()

	link := models.Link{
		UUID:      "550e8400-e29b-41d4-a716-446655440000",
		Thumbnail: "link-550e8400-e29b-41d4-a716-446655440000.webp",
		File: models.File{
			Thumbnail: "4x4.webp",
		},
	}

	got := ResolvedThumbnailURL(link)
	want := "/videos/qualitys/550e8400-e29b-41d4-a716-446655440000/image/thumb/link-550e8400-e29b-41d4-a716-446655440000.webp"
	if got != want {
		t.Fatalf("ResolvedThumbnailURL() = %q, want %q", got, want)
	}
}

func TestResolvedThumbnailURLFallsBackToFileThumbnail(t *testing.T) {
	restore := setThumbnailPublicRoot("/videos/qualitys")
	defer restore()

	link := models.Link{
		UUID: "550e8400-e29b-41d4-a716-446655440000",
		File: models.File{
			Thumbnail: "4x4.webp",
		},
	}

	got := ResolvedThumbnailURL(link)
	want := "/videos/qualitys/550e8400-e29b-41d4-a716-446655440000/image/thumb/4x4.webp"
	if got != want {
		t.Fatalf("ResolvedThumbnailURL() = %q, want %q", got, want)
	}
}

func TestGetThumbnailDataServesGeneratedFallback(t *testing.T) {
	root := setupThumbnailTestDB(t)
	link := createThumbnailTestLink(t, "550e8400-e29b-41d4-a716-446655440000", "", 1)
	writeThumbnailTestFile(t, root, link.File.UUID, "4x4.webp")

	status, filePath, userID, fileID, err := GetThumbnailData("4x4.webp", link.UUID)
	if err != nil {
		t.Fatalf("GetThumbnailData() error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if *filePath != filepath.Join(root, link.File.UUID, "4x4.webp") {
		t.Fatalf("filePath = %q, want generated thumbnail path", *filePath)
	}
	if userID != link.UserID || fileID != link.FileID {
		t.Fatalf("traffic IDs = user %d file %d, want user %d file %d", userID, fileID, link.UserID, link.FileID)
	}
}

func TestGetThumbnailDataServesCurrentLinkCustomPoster(t *testing.T) {
	root := setupThumbnailTestDB(t)
	customName := LinkThumbnailFilename("550e8400-e29b-41d4-a716-446655440000")
	link := createThumbnailTestLink(t, "550e8400-e29b-41d4-a716-446655440000", customName, 1)
	writeThumbnailTestFile(t, root, link.File.UUID, customName)

	status, filePath, _, _, err := GetThumbnailData(customName, link.UUID)
	if err != nil {
		t.Fatalf("GetThumbnailData() error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if *filePath != filepath.Join(root, link.File.UUID, customName) {
		t.Fatalf("filePath = %q, want custom thumbnail path", *filePath)
	}
}

func TestGetThumbnailDataRejectsAnotherLinksCustomPoster(t *testing.T) {
	root := setupThumbnailTestDB(t)
	link := createThumbnailTestLink(t, "550e8400-e29b-41d4-a716-446655440000", "", 1)
	otherName := LinkThumbnailFilename("550e8400-e29b-41d4-a716-446655440001")
	writeThumbnailTestFile(t, root, link.File.UUID, otherName)

	status, _, _, _, err := GetThumbnailData(otherName, link.UUID)
	if err == nil {
		t.Fatal("GetThumbnailData() error = nil, want error")
	}
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
	}
}

func TestResetLinkThumbnailClearsThumbnailAndRemovesFile(t *testing.T) {
	root := setupThumbnailTestDB(t)
	customName := LinkThumbnailFilename("550e8400-e29b-41d4-a716-446655440000")
	link := createThumbnailTestLink(t, "550e8400-e29b-41d4-a716-446655440000", customName, 1)
	customPath := writeThumbnailTestFile(t, root, link.File.UUID, customName)

	status, err := ResetLinkThumbnail(link.ID, link.UserID, false)
	if err != nil {
		t.Fatalf("ResetLinkThumbnail() error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}

	var updated models.Link
	if err := inits.DB.First(&updated, link.ID).Error; err != nil {
		t.Fatalf("failed to reload link: %v", err)
	}
	if updated.Thumbnail != "" {
		t.Fatalf("updated.Thumbnail = %q, want empty", updated.Thumbnail)
	}
	if _, err := os.Stat(customPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("custom thumbnail still exists or stat failed unexpectedly: %v", err)
	}
}

func TestLinkThumbnailUpdateAndResetRejectUnauthorizedUser(t *testing.T) {
	setupThumbnailTestDB(t)
	link := createThumbnailTestLink(t, "550e8400-e29b-41d4-a716-446655440000", LinkThumbnailFilename("550e8400-e29b-41d4-a716-446655440000"), 2)

	status, err := UpdateLinkThumbnail(link.ID, 1, false, strings.NewReader("not-used"), 8, "image/png")
	if err == nil {
		t.Fatal("UpdateLinkThumbnail() error = nil, want error")
	}
	if status != http.StatusForbidden {
		t.Fatalf("update status = %d, want %d", status, http.StatusForbidden)
	}

	status, err = ResetLinkThumbnail(link.ID, 1, false)
	if err == nil {
		t.Fatal("ResetLinkThumbnail() error = nil, want error")
	}
	if status != http.StatusForbidden {
		t.Fatalf("reset status = %d, want %d", status, http.StatusForbidden)
	}
}

func setupThumbnailTestDB(t *testing.T) string {
	t.Helper()

	previousDB := inits.DB
	previousPriv := config.ENV.FolderVideoQualitysPriv
	previousPub := config.ENV.FolderVideoQualitysPub
	previousMaxPostSize := config.ENV.MaxPostSize

	root := t.TempDir()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := db.AutoMigrate(&models.File{}, &models.Link{}); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	inits.DB = db
	config.ENV.FolderVideoQualitysPriv = root
	config.ENV.FolderVideoQualitysPub = "/videos/qualitys"
	config.ENV.MaxPostSize = 100 * 1024 * 1024

	t.Cleanup(func() {
		inits.DB = previousDB
		config.ENV.FolderVideoQualitysPriv = previousPriv
		config.ENV.FolderVideoQualitysPub = previousPub
		config.ENV.MaxPostSize = previousMaxPostSize
	})

	return root
}

func createThumbnailTestLink(t *testing.T, linkUUID string, linkThumbnail string, userID uint) models.Link {
	t.Helper()

	dbFile := models.File{
		UUID:      "file-" + linkUUID,
		Thumbnail: "4x4.webp",
		UserID:    userID,
	}
	if err := inits.DB.Create(&dbFile).Error; err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	dbLink := models.Link{
		UUID:      linkUUID,
		Thumbnail: linkThumbnail,
		UserID:    userID,
		FileID:    dbFile.ID,
		File:      dbFile,
	}
	if err := inits.DB.Create(&dbLink).Error; err != nil {
		t.Fatalf("failed to create link: %v", err)
	}
	dbLink.File = dbFile
	return dbLink
}

func writeThumbnailTestFile(t *testing.T, root string, fileUUID string, fileName string) string {
	t.Helper()

	path := filepath.Join(root, fileUUID, fileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create thumbnail directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("thumb"), 0o644); err != nil {
		t.Fatalf("failed to write thumbnail: %v", err)
	}
	return path
}

func setThumbnailPublicRoot(root string) func() {
	previous := config.ENV.FolderVideoQualitysPub
	config.ENV.FolderVideoQualitysPub = root
	return func() {
		config.ENV.FolderVideoQualitysPub = previous
	}
}
