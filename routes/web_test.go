package routes

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/controllers"
	"ch/kirari04/videocms/middlewares"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestWebRoutesUsePreparedDownloadJobsAndRemoveAttachmentRoute(t *testing.T) {
	ratelimit := false
	deps := &app.Deps{
		Snapshots: app.NewSnapshotStore(app.Snapshot{Config: config.Config{
			FolderVideoQualitysPub: "/media",
			RatelimitEnabled:       &ratelimit,
			RatelimitRateWeb:       1,
			RatelimitBurstWeb:      1,
		}}),
	}
	appServer := echo.New()
	Web(
		appServer,
		controllers.NewHandlers(deps, nil, nil, nil, nil),
		middlewares.NewFactory(deps, nil),
	)

	routes := make(map[string]bool)
	for _, route := range appServer.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, expected := range []string{
		http.MethodPost + " /media/:UUID/download-jobs",
		http.MethodGet + " /media/:UUID/download-jobs/:JOBUUID",
		http.MethodGet + " /media/:UUID/download-jobs/:JOBUUID/file",
		http.MethodGet + " /media/:UUID/:QUALITY/:STREAM/stream/video.mp4",
	} {
		if !routes[expected] {
			t.Fatalf("missing route %q", expected)
		}
	}
	if routes[http.MethodGet+" /media/:UUID/:QUALITY/download/:FILE"] {
		t.Fatal("legacy synchronous attachment route is still registered")
	}
}

func TestWebFrontendCacheControl(t *testing.T) {
	publicRoot := filepath.Join(t.TempDir(), "public")
	for name, contents := range map[string]string{
		"index.html":                              "index",
		"my/videos/index.html":                    "videos",
		"_nuxt/entry.Ca9_NHr6.css":                "css",
		"_nuxt/builds/latest.json":                `{}`,
		"_nuxt/builds/meta/build-identifier.json": `{}`,
	} {
		path := filepath.Join(publicRoot, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create fixture directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("create fixture file: %v", err)
		}
	}

	ratelimit := false
	deps := &app.Deps{
		Snapshots: app.NewSnapshotStore(app.Snapshot{Config: config.Config{
			FolderVideoQualitysPub: "/media",
			RatelimitEnabled:       &ratelimit,
			RatelimitRateWeb:       1,
			RatelimitBurstWeb:      1,
		}}),
	}
	appServer := echo.New()
	appServer.Filesystem = os.DirFS(filepath.Dir(publicRoot))
	Web(
		appServer,
		controllers.NewHandlers(deps, nil, nil, nil, nil),
		middlewares.NewFactory(deps, nil),
	)

	tests := []struct {
		path       string
		wantStatus int
		wantCache  string
	}{
		{path: "/", wantStatus: http.StatusOK, wantCache: frontendRevalidateCacheControl},
		{path: "/my/videos/", wantStatus: http.StatusOK, wantCache: frontendRevalidateCacheControl},
		{path: "/_nuxt/entry.Ca9_NHr6.css", wantStatus: http.StatusOK, wantCache: frontendImmutableCacheControl},
		{path: "/_nuxt/builds/latest.json", wantStatus: http.StatusOK, wantCache: frontendRevalidateCacheControl},
		{path: "/_nuxt/builds/meta/build-identifier.json", wantStatus: http.StatusOK, wantCache: frontendImmutableCacheControl},
		{path: "/_nuxt/missing.H4sh.js", wantStatus: http.StatusNotFound, wantCache: frontendRevalidateCacheControl},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			appServer.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if got := response.Header().Get("Cache-Control"); got != test.wantCache {
				t.Fatalf("Cache-Control = %q, want %q", got, test.wantCache)
			}
		})
	}
}
