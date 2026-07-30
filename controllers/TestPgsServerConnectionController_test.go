package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestPgsServerConnectionReachesServer(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodHead {
			t.Fatalf("expected HEAD request, got %s", request.Method)
		}
		response.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer target.Close()

	body, err := json.Marshal(pgsServerConnectionRequest{URL: target.URL})
	if err != nil {
		t.Fatal(err)
	}

	echoServer := echo.New()
	request := httptest.NewRequest(http.MethodPost, "/settings/test-pgs-server", bytes.NewReader(body))
	request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	recorder := httptest.NewRecorder()
	context := echoServer.NewContext(request, recorder)

	if err := (&Handlers{}).TestPgsServerConnection(context); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response pgsServerConnectionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK {
		t.Fatal("expected successful connection response")
	}
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected upstream status %d, got %d", http.StatusMethodNotAllowed, response.StatusCode)
	}
}

func TestPgsServerConnectionRejectsUnsupportedScheme(t *testing.T) {
	body := []byte(`{"url":"file:///tmp/subtitle-server"}`)

	echoServer := echo.New()
	request := httptest.NewRequest(http.MethodPost, "/settings/test-pgs-server", bytes.NewReader(body))
	request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	recorder := httptest.NewRecorder()
	context := echoServer.NewContext(request, recorder)

	if err := (&Handlers{}).TestPgsServerConnection(context); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}
