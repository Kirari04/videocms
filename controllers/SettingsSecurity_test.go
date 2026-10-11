package controllers

import (
	"ch/kirari04/videocms/configdb"
	"ch/kirari04/videocms/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

func settingsSecurityHandlers(t *testing.T) (*Handlers, models.Setting) {
	t.Helper()
	h, _ := newSessionHandlers(t)
	if err := h.Deps.DB.AutoMigrate(&models.Setting{}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := configdb.LoadSnapshot(h.Deps.DB, h.Config())
	if err != nil {
		t.Fatal(err)
	}
	h.Deps.Snapshots.Replace(snapshot)
	var row models.Setting
	if err := h.Deps.DB.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	row.CaptchaType = "turnstile"
	row.Captcha_Turnstile_PublicKey = "public-key"
	row.Captcha_Turnstile_PrivateKey = "stored-private-key"
	if err := h.Deps.DB.Save(&row).Error; err != nil {
		t.Fatal(err)
	}
	return h, row
}

func securityRequest(t *testing.T, handler echo.HandlerFunc, method string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/api/settings", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.Set("UserID", uint(1))
	c.Set("Admin", true)
	if err := handler(c); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestSettingsRedactionRoundTripPreservesSecrets(t *testing.T) {
	h, stored := settingsSecurityHandlers(t)
	rec := securityRequest(t, h.GetSettings, http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET settings: %d", rec.Code)
	}
	for _, secret := range []string{stored.JwtSecretKey, stored.JwtMediaSecretKey, stored.Captcha_Turnstile_PrivateKey} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatal("GET settings exposed a secret")
		}
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["JwtSecretKey"]; ok {
		t.Fatal("signing key is present in DTO")
	}
	if body["Captcha_Turnstile_PrivateKeyConfigured"] != true {
		t.Fatal("saved captcha key presence missing")
	}
	body["AppName"] = "Updated name"
	body["JwtSecretKey"] = "attacker-selected-signing-key"
	body["JwtMediaSecretKey"] = "attacker-selected-media-key"
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rec = securityRequest(t, h.UpdateSettings, http.MethodPut, string(payload))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT redacted settings: %d %s", rec.Code, rec.Body.String())
	}
	var current models.Setting
	if err := h.Deps.DB.First(&current).Error; err != nil {
		t.Fatal(err)
	}
	if current.AppName != "Updated name" || current.JwtSecretKey != stored.JwtSecretKey || current.JwtMediaSecretKey != stored.JwtMediaSecretKey || current.Captcha_Turnstile_PrivateKey != stored.Captcha_Turnstile_PrivateKey {
		t.Fatal("settings round-trip lost secrets or failed to save public configuration")
	}
	body["Captcha_Turnstile_PrivateKey"] = "replacement-private-key"
	payload, err = json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rec = securityRequest(t, h.UpdateSettings, http.MethodPut, string(payload))
	if rec.Code != http.StatusOK {
		t.Fatalf("replace captcha key: %d %s", rec.Code, rec.Body.String())
	}
	if err := h.Deps.DB.First(&current).Error; err != nil {
		t.Fatal(err)
	}
	if current.Captcha_Turnstile_PrivateKey != "replacement-private-key" {
		t.Fatal("captcha key replacement failed")
	}
}

func TestLoginFailureDoesNotDiscloseAccountExistence(t *testing.T) {
	h, user := newSessionHandlers(t)
	var failureBody string
	for _, username := range []string{user.Username, "missing-user"} {
		rec := securityRequest(t, h.AuthLogin, http.MethodPost, `{"Username":"`+username+`","Password":"wrong-password"}`)
		if rec.Code != http.StatusBadRequest || rec.Body.String() != "Invalid credentials" {
			t.Fatalf("login disclosed account existence: %d %s", rec.Code, rec.Body.String())
		}
		if failureBody != "" && rec.Body.String() != failureBody {
			t.Fatal("login errors differ")
		}
		failureBody = rec.Body.String()
	}
	sessionLogin(t, h, user.Username, "original-password")
}

func TestDummyLoginHashPerformsProductionPasswordWork(t *testing.T) {
	if cost, err := bcrypt.Cost([]byte(dummyLoginHash)); err != nil || cost != 14 {
		t.Fatalf("dummy cost: %d %v", cost, err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(dummyLoginHash), []byte("wrong-password")); err != bcrypt.ErrMismatchedHashAndPassword {
		t.Fatalf("dummy hash did not perform bcrypt: %v", err)
	}
}

func TestSigningKeyRotationIsExplicitAndRevokesOldTokens(t *testing.T) {
	h, stored := settingsSecurityHandlers(t)
	var user models.User
	if err := h.Deps.DB.First(&user).Error; err != nil {
		t.Fatal(err)
	}
	old, _, err := h.Auth.GenerateJWT(user)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{}`, `{"confirm":false}`} {
		rec := securityRequest(t, h.RotateSigningKeys, http.MethodPost, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("unconfirmed rotation: %d", rec.Code)
		}
		if h.Config().JwtSecretKey != stored.JwtSecretKey {
			t.Fatal("unconfirmed rotation changed key")
		}
	}
	rec := securityRequest(t, h.RotateSigningKeys, http.MethodPost, `{"confirm":true}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("rotation: %d %s", rec.Code, rec.Body.String())
	}
	if h.Config().JwtSecretKey == stored.JwtSecretKey || h.Config().JwtMediaSecretKey == stored.JwtMediaSecretKey {
		t.Fatal("rotation did not replace both keys")
	}
	if _, _, err := h.Auth.VerifyJWT(old); err == nil {
		t.Fatal("old session survived signing-key rotation")
	}
	sessionLogin(t, h, user.Username, "original-password")
}
