package controllers

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/models"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRenderWebPageContentMarkdown(t *testing.T) {
	rendered, err := RenderWebPageContent(
		models.WebPageFormatMarkdown,
		"# Privacy\n\nThis is **important**.\n\n- One\n- Two",
	)
	if err != nil {
		t.Fatalf("RenderWebPageContent() error = %v", err)
	}

	for _, expected := range []string{
		`<h1 id="privacy">Privacy</h1>`,
		"<strong>important</strong>",
		"<li>One</li>",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("rendered content does not contain %q:\n%s", expected, rendered)
		}
	}
}

func TestRenderWebPageContentSanitizesHTML(t *testing.T) {
	rendered, err := RenderWebPageContent(
		models.WebPageFormatHTML,
		`<section class="vc-section" onclick="alert(1)"><script>alert(1)</script><a href="javascript:alert(1)">Unsafe</a><p style="position:fixed">Safe</p></section>`,
	)
	if err != nil {
		t.Fatalf("RenderWebPageContent() error = %v", err)
	}

	if !strings.Contains(rendered, `class="vc-section"`) {
		t.Fatalf("expected VideoCMS compatibility class to be preserved: %s", rendered)
	}
	for _, unsafe := range []string{"onclick", "<script", "javascript:", "position:fixed"} {
		if strings.Contains(rendered, unsafe) {
			t.Fatalf("rendered content contains unsafe value %q: %s", unsafe, rendered)
		}
	}
	if !strings.Contains(rendered, "<p>Safe</p>") {
		t.Fatalf("expected safe HTML to remain: %s", rendered)
	}
}

func TestGetPublicWebPageHidesUnpublishedPage(t *testing.T) {
	h := webPageTestHandlers(t)
	if err := h.Deps.DB.Create(&models.WebPage{
		Path:      "/hidden/",
		Title:     "Hidden",
		Content:   "Secret",
		Format:    models.WebPageFormatMarkdown,
		Published: false,
	}).Error; err != nil {
		t.Fatalf("create webpage: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(
		http.MethodGet,
		"/p/page?Path="+url.QueryEscape("/hidden/"),
		nil,
	)
	rec := httptest.NewRecorder()

	if err := h.GetPublicWebPage(e.NewContext(req, rec)); err != nil {
		t.Fatalf("GetPublicWebPage() error = %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestListPublicWebPageOnlyReturnsPublishedPages(t *testing.T) {
	h := webPageTestHandlers(t)
	pages := []models.WebPage{
		{
			Path:      "/visible/",
			Title:     "Visible",
			Content:   "Visible content",
			Format:    models.WebPageFormatMarkdown,
			Published: true,
		},
		{
			Path:      "/hidden/",
			Title:     "Hidden",
			Content:   "Hidden content",
			Format:    models.WebPageFormatMarkdown,
			Published: false,
		},
	}
	if err := h.Deps.DB.Create(&pages).Error; err != nil {
		t.Fatalf("create webpages: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/p/pages", nil)
	rec := httptest.NewRecorder()

	if err := h.ListPublicWebPage(e.NewContext(req, rec)); err != nil {
		t.Fatalf("ListPublicWebPage() error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "Visible") {
		t.Fatalf("published page missing from response: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Hidden") {
		t.Fatalf("unpublished page leaked in response: %s", rec.Body.String())
	}
}

func TestNormalizeWebPagePath(t *testing.T) {
	path, err := normalizeWebPagePath(" /legal/privacy/ ")
	if err != nil {
		t.Fatalf("normalizeWebPagePath() error = %v", err)
	}
	if path != "/legal/privacy/" {
		t.Fatalf("path = %q, want %q", path, "/legal/privacy/")
	}
}

func webPageTestHandlers(t *testing.T) *Handlers {
	t.Helper()

	db, err := gorm.Open(
		sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"),
		&gorm.Config{},
	)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(&models.WebPage{}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}

	return &Handlers{
		Deps: &app.Deps{
			DB:        db,
			Snapshots: app.NewSnapshotStore(app.Snapshot{}),
		},
	}
}
