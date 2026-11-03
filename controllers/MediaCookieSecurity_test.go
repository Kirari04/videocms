package controllers

import (
	"ch/kirari04/videocms/middlewares"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestMediaCookiesOnlyTrustHTTPSFromConfiguredPeers(t *testing.T) {
	_, trusted, _ := net.ParseCIDR("192.0.2.0/24")
	for _, tc := range []struct {
		name, peer, proto, base string
		tls, local, secure      bool
	}{
		{"untrusted spoof", "198.51.100.1:1234", "https", "http://example.com", false, false, false},
		{"trusted proxy", "192.0.2.1:1234", "https", "http://example.com", false, false, true},
		{"untrusted localhost", "127.0.0.1:1234", "https", "http://example.com", false, false, false},
		{"trusted local", "127.0.0.1:1234", "https", "http://example.com", false, true, true},
		{"TLS", "198.51.100.1:1234", "http", "http://example.com", true, false, true},
		{"canonical HTTPS", "198.51.100.1:1234", "http", "https://example.com", false, false, true},
		{"malformed peer", "bad-peer", "https", "http://example.com", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newSessionHandlers(t)
			snapshot := h.Deps.Snapshots.Current()
			snapshot.Config.BaseUrl = tc.base
			h.Deps.Snapshots.Replace(snapshot)
			e := echo.New()
			e.Use(middlewares.TrustedProxyHTTPS([]*net.IPNet{trusted}, tc.local))
			e.GET("/", func(c echo.Context) error {
				c.SetCookie(h.mediaCookie(c, "link", "token", time.Now().Add(time.Hour)))
				return c.NoContent(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.peer
			req.Header.Set("X-Forwarded-Proto", tc.proto)
			if tc.tls {
				req.TLS = &tls.ConnectionState{}
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			cookie := rec.Result().Cookies()[0]
			if cookie.Secure != tc.secure {
				t.Fatalf("Secure=%v", cookie.Secure)
			}
			if !cookie.HttpOnly || cookie.Secure && cookie.SameSite != http.SameSiteNoneMode || !cookie.Secure && cookie.SameSite != http.SameSiteLaxMode {
				t.Fatal("cookie attributes changed")
			}
		})
	}
}
