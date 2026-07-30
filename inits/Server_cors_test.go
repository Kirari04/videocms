package inits

import (
	"ch/kirari04/videocms/config"
	"net/http"
	"net/http/httptest"
	"reflect"
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
