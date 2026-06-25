package middlewares

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/config"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestMediaAuthRejectsMissingCookie(t *testing.T) {
	rec := runMediaAuth(t, "link-uuid", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMediaAuthRejectsInvalidCookie(t *testing.T) {
	rec := runMediaAuth(t, "link-uuid", "not-a-token")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMediaAuthRejectsUUIDMismatch(t *testing.T) {
	tokenString := mediaAuthToken(t, "link-uuid")
	rec := runMediaAuth(t, "other-link-uuid", tokenString)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestMediaAuthStoresClaims(t *testing.T) {
	tokenString := mediaAuthToken(t, "link-uuid")
	rec := runMediaAuth(t, "link-uuid", tokenString)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func runMediaAuth(t *testing.T, routeUUID string, cookieValue string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/videos/qualitys/"+routeUUID+"/stream/multi/master.m3u8", nil)
	if cookieValue != "" {
		req.AddCookie(&http.Cookie{Name: auth.MediaCookieName, Value: cookieValue})
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("UUID")
	c.SetParamValues(routeUUID)

	factory := newMediaAuthFactory()
	handler := factory.MediaAuth()(func(c echo.Context) error {
		claims, ok := MediaClaims(c)
		if !ok {
			t.Fatal("media claims were not stored in context")
		}
		if claims.LinkUUID != "link-uuid" {
			t.Fatalf("claims.LinkUUID = %q, want %q", claims.LinkUUID, "link-uuid")
		}
		return c.NoContent(http.StatusNoContent)
	})
	if err := handler(c); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	return rec
}

func mediaAuthToken(t *testing.T, linkUUID string) string {
	t.Helper()
	factory := newMediaAuthFactory()

	tokenString, _, err := factory.Auth.GenerateMediaToken(auth.MediaClaims{
		LinkUUID: linkUUID,
		FileUUID: "file-uuid",
	})
	if err != nil {
		t.Fatalf("GenerateMediaToken() error = %v", err)
	}
	return tokenString
}

func newMediaAuthFactory() *Factory {
	deps := &app.Deps{
		Snapshots: app.NewSnapshotStore(app.Snapshot{
			Config: config.Config{
				JwtSecretKey:      "jwt-secret",
				JwtMediaSecretKey: "media-secret",
			},
		}),
	}
	return NewFactory(deps, auth.NewService(deps))
}
