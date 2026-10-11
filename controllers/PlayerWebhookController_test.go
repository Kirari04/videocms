package controllers

import (
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/middlewares"
	"ch/kirari04/videocms/models"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

type webhookTransport func(*http.Request) (*http.Response, error)

func (f webhookTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPlayerWebhooksStayServerSideAndOwnerScoped(t *testing.T) {
	h, user := newSessionHandlers(t)
	if err := h.Deps.DB.AutoMigrate(&models.Link{}, &models.Webhook{}); err != nil {
		t.Fatal(err)
	}
	link := models.Link{UUID: testLinkUUID, UserID: user.ID}
	hook := models.Webhook{UserID: user.ID, Url: "https://hooks.example/progress", Rpm: 2, ReqQuery: "student", ResField: "student"}
	foreign := models.Webhook{UserID: user.ID + 1, Url: "https://foreign.example/progress", ResField: "student"}
	for _, row := range []any{&link, &hook, &foreign} {
		if err := h.Deps.DB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	h.webhookHTTPClient = &http.Client{Transport: webhookTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != hook.Url || r.Method != http.MethodPost {
			t.Fatal("viewer changed webhook destination")
		}
		if _, ok := r.Context().Deadline(); !ok {
			t.Fatal("webhook has no deadline")
		}
		if err := r.ParseMultipartForm(4096); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("uuid") != link.UUID || r.FormValue("position") != "15.4" || r.FormValue("student") != "john" {
			t.Fatalf("multipart compatibility lost: %v", r.Form)
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Fatal("viewer credentials sent to webhook")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
	})}
	run := func(id uint, body string, authorized bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/media/"+link.UUID+"/webhooks/"+strconv.Itoa(int(id)), strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(req, rec)
		c.SetParamNames("UUID", "HOOKID")
		c.SetParamValues(link.UUID, strconv.Itoa(int(id)))
		if authorized {
			c.Set(middlewares.MediaClaimsContextKey, &auth.MediaClaims{LinkUUID: link.UUID, UserID: user.ID})
		}
		if err := h.DispatchPlayerWebhook(c); err != nil {
			t.Fatal(err)
		}
		return rec
	}
	if rec := run(hook.ID, `{"position":15.4,"value":"john","url":"http://127.0.0.1/"}`, true); rec.Code != http.StatusNoContent {
		t.Fatalf("public webhook: %d", rec.Code)
	}
	for _, tc := range []struct {
		id         uint
		body       string
		authorized bool
		want       int
	}{
		{foreign.ID, `{"position":15.4}`, true, http.StatusNotFound},
		{hook.ID, `{"position":15.4}`, false, http.StatusUnauthorized},
		{hook.ID, `{"position":-1}`, true, http.StatusBadRequest},
		{hook.ID, fmt.Sprintf(`{"value":"%s"}`, strings.Repeat("a", 513)), true, http.StatusBadRequest},
	} {
		if rec := run(tc.id, tc.body, tc.authorized); rec.Code != tc.want {
			t.Fatalf("invalid webhook: %d want %d", rec.Code, tc.want)
		}
	}
	if calls != 1 {
		t.Fatalf("invalid requests caused %d outgoing calls", calls)
	}
	// Use the real guarded client for local, DNS-resolved and reserved targets.
	h.webhookHTTPClient = nil
	for _, target := range []string{"http://127.0.0.1/", "http://localhost/", "http://100.64.0.1/", "http://[::ffff:100.64.0.1]/", "http://198.18.0.1/"} {
		if err := h.Deps.DB.Model(&hook).Update("url", target).Error; err != nil {
			t.Fatal(err)
		}
		if rec := run(hook.ID, `{"position":15.4}`, true); rec.Code != http.StatusBadGateway {
			t.Fatalf("blocked target %s got %d", target, rec.Code)
		}
	}
}
