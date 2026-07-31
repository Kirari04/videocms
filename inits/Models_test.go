package inits

import (
	"ch/kirari04/videocms/models"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type legacyWebPage struct {
	models.Model
	Path         string
	Title        string
	HTML         string `gorm:"column:html"`
	ListInFooter bool
}

type legacyStoredFile struct {
	models.Model
	UUID string
	Path string
}

func (legacyStoredFile) TableName() string {
	return "files"
}

func (legacyWebPage) TableName() string {
	return "web_pages"
}

func TestMigrateModelsPreservesLegacyWebPages(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"),
		&gorm.Config{},
	)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(&legacyWebPage{}); err != nil {
		t.Fatalf("migrate legacy webpage: %v", err)
	}
	if err := db.Create(&legacyWebPage{
		Path:         "/privacy/",
		Title:        "Privacy",
		HTML:         "<p>Legacy content</p>",
		ListInFooter: true,
	}).Error; err != nil {
		t.Fatalf("create legacy webpage: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE traffic_logs (
			id integer PRIMARY KEY AUTOINCREMENT,
			created_at datetime,
			updated_at datetime,
			deleted_at datetime,
			user_id integer,
			file_id integer,
			quality_id integer,
			audio_id integer,
			bytes integer
		)
	`).Error; err != nil {
		t.Fatalf("create legacy traffic table: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO traffic_logs (created_at, user_id, file_id, quality_id, audio_id, bytes)
		VALUES (CURRENT_TIMESTAMP, 1, 2, 3, 4, 512)
	`).Error; err != nil {
		t.Fatalf("create legacy traffic row: %v", err)
	}
	if err := MigrateModels(db); err != nil {
		t.Fatalf("MigrateModels() error = %v", err)
	}

	var page models.WebPage
	if err := db.First(&page).Error; err != nil {
		t.Fatalf("load migrated webpage: %v", err)
	}
	if page.Content != "<p>Legacy content</p>" {
		t.Fatalf("content = %q, want legacy HTML", page.Content)
	}
	if page.Format != models.WebPageFormatHTML {
		t.Fatalf("format = %q, want %q", page.Format, models.WebPageFormatHTML)
	}
	if !page.Published {
		t.Fatal("expected legacy webpage to remain published")
	}

	var traffic models.TrafficLog
	if err := db.First(&traffic).Error; err != nil {
		t.Fatalf("load migrated traffic: %v", err)
	}
	if traffic.Source != models.TrafficSourcePlayer || traffic.Bytes != 512 {
		t.Fatalf("migrated traffic = %#v, want player source with 512 bytes", traffic)
	}
}

func TestMigrateModelsBackfillsLegacyFileStorageID(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"),
		&gorm.Config{},
	)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(&legacyStoredFile{}); err != nil {
		t.Fatalf("migrate legacy file: %v", err)
	}
	if err := db.Create(&legacyStoredFile{UUID: "legacy-file", Path: "/legacy/source.mp4"}).Error; err != nil {
		t.Fatalf("create legacy file: %v", err)
	}
	if err := MigrateModels(db); err != nil {
		t.Fatalf("MigrateModels() error = %v", err)
	}

	var file models.File
	if err := db.Where("uuid = ?", "legacy-file").First(&file).Error; err != nil {
		t.Fatalf("load migrated file: %v", err)
	}
	if file.StorageID != "local" {
		t.Fatalf("storage ID = %q, want local", file.StorageID)
	}
	if file.SourceKey != "" || file.Path != "/legacy/source.mp4" {
		t.Fatalf("legacy source fields changed unexpectedly: %#v", file)
	}
}
