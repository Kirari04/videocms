package routes

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/controllers"
	"ch/kirari04/videocms/middlewares"
	"net/http"
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
