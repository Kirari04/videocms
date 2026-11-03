package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	"github.com/labstack/echo/v4"
)

func TestGetConfigOnlyExposesClientSettingsAndActiveCaptchaKey(t *testing.T) {
	for _, tc := range []struct {
		name, provider string
		enabled        bool
		key, value     string
	}{
		{"recaptcha", "recaptcha", true, "Captcha_Recaptcha_PublicKey", "recaptcha-public"},
		{"hcaptcha", "hcaptcha", true, "Captcha_Hcaptcha_PublicKey", "hcaptcha-public"},
		{"turnstile", "turnstile", true, "Captcha_Turnstile_PublicKey", "turnstile-public"},
		{"disabled recaptcha", "recaptcha", false, "", ""},
		{"disabled hcaptcha", "hcaptcha", false, "", ""},
		{"disabled turnstile", "turnstile", false, "", ""},
		{"unknown provider", "unknown", true, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enabled, disabled := true, false
			cfg := config.Config{
				AppName: "VideoCMS", BaseUrl: "https://configured.example",
				UploadEnabled: &disabled, RemoteDownloadEnabled: &enabled,
				MaxUploadFilesize: 4096, MaxUploadChunkSize: 1024, MaxUploadSessions: 10,
				FolderVideoQualitysPub: "/custom-media", FolderVideoQualitysPriv: "/private/media",
				JwtSecretKey: "signing-secret", JwtMediaSecretKey: "media-secret",
				CaptchaEnabled: &tc.enabled, CaptchaLoginEnabled: &disabled, CaptchaType: tc.provider,
				Captcha_Recaptcha_PublicKey: "recaptcha-public", Captcha_Recaptcha_PrivateKey: "recaptcha-secret",
				Captcha_Hcaptcha_PublicKey: "hcaptcha-public", Captcha_Hcaptcha_PrivateKey: "hcaptcha-secret",
				Captcha_Turnstile_PublicKey: "turnstile-public", Captcha_Turnstile_PrivateKey: "turnstile-secret",
			}
			h := NewHandlers(&app.Deps{Snapshots: app.NewSnapshotStore(app.Snapshot{Config: cfg})}, nil, nil, nil, nil)
			server := echo.New()
			server.GET("/api/config", h.GetConfig)
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET config: %d %s", rec.Code, rec.Body.String())
			}
			var response map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{
				"AppName": "VideoCMS", "UploadEnabled": false, "RemoteDownloadEnabled": true,
				"MaxUploadFilesize": float64(4096), "MaxUploadChunkSize": float64(1024),
				"CaptchaEnabled": tc.enabled, "CaptchaLoginEnabled": false, "CaptchaType": tc.provider,
			}
			if tc.key != "" {
				want[tc.key] = tc.value
			}
			if !reflect.DeepEqual(response, want) {
				t.Fatalf("public config = %#v, want %#v", response, want)
			}
		})
	}
}
