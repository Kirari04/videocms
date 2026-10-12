package inits

import (
	"ch/kirari04/videocms/config"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func TestParseCORSAllowOrigins(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{
			name:  "wildcard",
			value: "*",
			want:  []string{"*"},
		},
		{
			name:  "single origin",
			value: "https://admin.example.com",
			want:  []string{"https://admin.example.com"},
		},
		{
			name:  "multiple origins with whitespace",
			value: " https://admin.example.com,https://app.example.com , http://localhost:3000 ",
			want: []string{
				"https://admin.example.com",
				"https://app.example.com",
				"http://localhost:3000",
			},
		},
		{
			name:  "empty entries and duplicates",
			value: "https://admin.example.com,, https://admin.example.com,",
			want:  []string{"https://admin.example.com"},
		},
		{
			name:  "only empty entries fails closed",
			value: " , , ",
			want:  []string{""},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseCORSAllowOrigins(test.value); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("parseCORSAllowOrigins(%q) = %#v, want %#v", test.value, got, test.want)
			}
		})
	}
}

func TestSharedTusPreflightAllowsGuestHeaderAndCredentials(t *testing.T) {
	enabled := true
	app := echo.New()
	app.Use(middleware.CORSWithConfig(serverCORSConfig(config.Config{CorsAllowOrigins: "https://app.example.test", CorsAllowCredentials: &enabled})))
	app.PATCH("/api/uploads/:id", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodOptions, "/api/uploads/example", nil)
	req.Header.Set(echo.HeaderOrigin, "https://app.example.test")
	req.Header.Set(echo.HeaderAccessControlRequestMethod, http.MethodPatch)
	req.Header.Set(echo.HeaderAccessControlRequestHeaders, "X-Folder-Share,Tus-Resumable,Upload-Offset,Content-Type")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if rec.Code != 204 || rec.Header().Get(echo.HeaderAccessControlAllowCredentials) != "true" || !strings.Contains(rec.Header().Get(echo.HeaderAccessControlAllowHeaders), "X-Folder-Share") {
		t.Fatalf("guest tus preflight: %d %v", rec.Code, rec.Header())
	}
}

func TestServerCORSConfigAllowsEachConfiguredOrigin(t *testing.T) {
	allowCredentials := true
	env := config.Config{
		CorsAllowOrigins:     " https://admin.example.com, https://app.example.com ",
		CorsAllowHeaders:     "*",
		CorsAllowCredentials: &allowCredentials,
	}

	app := echo.New()
	app.Use(middleware.CORSWithConfig(serverCORSConfig(env)))
	app.GET("/", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	tests := []struct {
		origin string
		want   string
	}{
		{origin: "https://admin.example.com", want: "https://admin.example.com"},
		{origin: "https://app.example.com", want: "https://app.example.com"},
		{origin: "https://blocked.example.com", want: ""},
	}

	for _, test := range tests {
		t.Run(test.origin, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set(echo.HeaderOrigin, test.origin)
			response := httptest.NewRecorder()

			app.ServeHTTP(response, request)

			if got := response.Header().Get(echo.HeaderAccessControlAllowOrigin); got != test.want {
				t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, test.want)
			}
		})
	}
}
