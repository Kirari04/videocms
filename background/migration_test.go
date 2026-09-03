package background

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ch/kirari04/videocms/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrateUpgradesLegacyIdempotencyIndex(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy-index.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE background_jobs (id text PRIMARY KEY, idempotency_key text)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX idx_background_job_idempotency ON background_jobs(idempotency_key)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO background_jobs(id, idempotency_key) VALUES (?, '')", "legacy-empty").Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var indexSQL string
	if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?", "idx_background_job_idempotency").Scan(&indexSQL).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(indexSQL), " where ") {
		t.Fatalf("idempotency index is not partial: %q", indexSQL)
	}
	if err := db.Exec("INSERT INTO background_jobs(id, idempotency_key) VALUES (?, '')", "second-empty").Error; err != nil {
		t.Fatalf("partial index still rejected an empty key: %v", err)
	}
}

func TestBackfillLegacyIsDurableAndIdempotent(t *testing.T) {
	_, db := testRuntime(t)
	if err := db.AutoMigrate(
		&models.File{}, &models.Link{}, &models.Quality{}, &models.Audio{}, &models.Subtitle{},
		&models.RemoteDownload{}, &models.DownloadJob{}, &models.UploadSession{}, &models.UploadLog{},
	); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	row := models.RemoteDownload{
		UserID: 7, Url: "https://example.test/video.mp4", Name: "video.mp4",
		Status: models.RemoteDownloadStatusFailed, Error: "legacy failure",
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := BackfillLegacy(db, now); err != nil {
		t.Fatal(err)
	}
	if err := BackfillLegacy(db, now.Add(time.Hour)); err != nil {
		t.Fatalf("idempotent replay failed: %v", err)
	}
	var jobs int64
	if err := db.Model(&Job{}).Where("idempotency_key = ?", fmt.Sprintf("legacy:remote-download:%d", row.ID)).Count(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if jobs != 1 {
		t.Fatalf("backfill created %d jobs, want 1", jobs)
	}
	var marker MigrationState
	if err := db.First(&marker, "key = ?", legacyCutoverMigration).Error; err != nil {
		t.Fatal(err)
	}
	if marker.CompletedAt.IsZero() {
		t.Fatal("migration marker has no completion time")
	}
}
