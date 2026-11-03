package middlewares

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestSecurityHeadersPreservePublicEmbeddingAndProtectAdmin(t *testing.T) {
	e := echo.New()
	e.Use(SecurityHeaders())
	e.GET("/*", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	for _, path := range []string{"/my/config", "/api/settings", "/_nuxt/app.js", "/v/video", "/v/video/download", "/captcha/challenge", "/captcha/verify"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatalf("missing headers for %s", path)
		}
		csp := rec.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "object-src 'none'") || !strings.Contains(csp, "worker-src 'self' blob:") || !strings.Contains(csp, "connect-src 'self' blob:") {
			t.Fatal("missing baseline CSP, media workers or subtitle loading")
		}
		if !strings.Contains(csp, "connect-src 'self' blob: https://api.iconify.design ") {
			t.Fatal("frontend icon provider blocked")
		}
		embedded := strings.HasPrefix(path, "/v/") || strings.HasPrefix(path, "/captcha/")
		if embedded {
			if rec.Header().Get("X-Frame-Options") != "" || !strings.Contains(csp, "frame-ancestors *") {
				t.Fatalf("public embed blocked: %s", path)
			}
		} else if rec.Header().Get("X-Frame-Options") != "DENY" || !strings.Contains(csp, "frame-ancestors 'none'") {
			t.Fatalf("admin/static framing allowed: %s", path)
		}
	}
}
