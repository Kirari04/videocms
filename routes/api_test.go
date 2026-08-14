package routes

import (
	"net/http"
	"testing"

	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/controllers"
	"ch/kirari04/videocms/middlewares"
	"github.com/labstack/echo/v4"
)

func TestAPIRoutesKeepLegacyContractsAlongsideBackgroundJobV2Routes(t *testing.T) {
	ratelimit := false
	uploads := true
	deps := &app.Deps{Snapshots: app.NewSnapshotStore(app.Snapshot{Config: config.Config{
		UploadEnabled:        &uploads,
		RatelimitEnabled:     &ratelimit,
		RatelimitRateAuth:    1,
		RatelimitBurstAuth:   1,
		RatelimitRateApi:     1,
		RatelimitBurstApi:    1,
		RatelimitRateUpload:  1,
		RatelimitBurstUpload: 1,
		MaxUploadFilesize:    1024,
		MaxUploadChunkSize:   1024,
		MaxItemsMultiDelete:  100,
	}})}

	server := echo.New()
	Api(
		server.Group("/api"),
		controllers.NewHandlers(deps, nil, nil, nil, nil),
		middlewares.NewFactory(deps, nil),
	)

	routes := make(map[string]bool)
	for _, route := range server.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, pair := range [][2]string{
		{http.MethodPost + " /api/file/upload", http.MethodPost + " /api/v2/uploads/simple"},
		{http.MethodPost + " /api/uploads/:upload_id/finalize", http.MethodPost + " /api/v2/uploads/:upload_id/finalize"},
		{http.MethodPost + " /api/remote/download", http.MethodPost + " /api/v2/remote-downloads"},
		{http.MethodPost + " /api/remote/download/:id/cancel", http.MethodPost + " /api/v2/remote-downloads/:id/cancel"},
		{http.MethodPost + " /api/remote/download/:id/retry", http.MethodPost + " /api/v2/remote-downloads/:id/retry"},
		{http.MethodDelete + " /api/file", http.MethodDelete + " /api/v2/file"},
		{http.MethodDelete + " /api/files", http.MethodDelete + " /api/v2/files"},
		{http.MethodDelete + " /api/folder", http.MethodDelete + " /api/v2/folder"},
		{http.MethodDelete + " /api/folders", http.MethodDelete + " /api/v2/folders"},
	} {
		for _, expected := range pair {
			if !routes[expected] {
				t.Errorf("missing compatibility route %q", expected)
			}
		}
	}
}
