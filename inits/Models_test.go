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
}
