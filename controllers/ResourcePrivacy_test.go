package controllers

import (
	"ch/kirari04/videocms/models"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestLibraryRequestsHideForeignResourceExistence(t *testing.T) {
	h, user := newSessionHandlers(t)
	if err := h.Deps.DB.AutoMigrate(&models.Folder{}, &models.Link{}, &models.File{}, &models.Quality{}, &models.Tag{}, &models.TagLinks{}); err != nil {
		t.Fatal(err)
	}
	folder := models.Folder{UserID: user.ID + 1, Name: "foreign"}
	link := models.Link{UserID: user.ID + 1, Name: "foreign"}
	if err := h.Deps.DB.Create(&folder).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.Deps.DB.Create(&link).Error; err != nil {
		t.Fatal(err)
	}
	run := func(handler echo.HandlerFunc, method, url, body string, admin bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(req, rec)
		c.Set("UserID", user.ID)
		c.Set("Admin", admin)
		if err := handler(c); err != nil {
			t.Fatal(err)
		}
		return rec
	}
	for _, tc := range []struct {
		name      string
		handler   echo.HandlerFunc
		method    string
		foreign   uint
		url, body string
	}{
		{"list files", h.ListFiles, http.MethodGet, folder.ID, "/api/files?ParentFolderID=%d", ""},
		{"list folders", h.ListFolders, http.MethodGet, folder.ID, "/api/folders?ParentFolderID=%d", ""},
		{"update file", h.UpdateFile, http.MethodPut, link.ID, "/api/file", `{"LinkID":%d,"Name":"updated"}`},
		{"update folder", h.UpdateFolder, http.MethodPut, folder.ID, "/api/folder", `{"FolderID":%d,"Name":"updated"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var first *httptest.ResponseRecorder
			for _, id := range []uint{tc.foreign, 99999} {
				url, body := tc.url, tc.body
				if strings.Contains(url, "%d") {
					url = fmt.Sprintf(url, id)
				}
				if body != "" {
					body = fmt.Sprintf(body, id)
				}
				rec := run(tc.handler, tc.method, url, body, false)
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("foreign/missing resource: %d %s", rec.Code, rec.Body.String())
				}
				if first != nil && (rec.Code != first.Code || rec.Body.String() != first.Body.String()) {
					t.Fatal("foreign and missing responses differ")
				}
				first = rec
			}
			if tc.method == http.MethodPut {
				rec := run(tc.handler, tc.method, tc.url, fmt.Sprintf(tc.body, tc.foreign), true)
				if rec.Code != http.StatusOK {
					t.Fatalf("admin update: %d %s", rec.Code, rec.Body.String())
				}
			}
		})
	}
	for _, handler := range []echo.HandlerFunc{h.ListFiles, h.ListFolders} {
		for _, admin := range []bool{false, true} {
			rec := run(handler, http.MethodGet, fmt.Sprintf("/api/list?ParentFolderID=%d&UserID=%d", folder.ID, folder.UserID), "", admin)
			want := http.StatusBadRequest
			if admin {
				want = http.StatusOK
			}
			if rec.Code != want {
				t.Fatalf("selected-owner browse: %d %s", rec.Code, rec.Body.String())
			}
			rec = run(handler, http.MethodGet, "/api/list?ParentFolderID=0", "", admin)
			if rec.Code != http.StatusOK {
				t.Fatalf("root listing: %d %s", rec.Code, rec.Body.String())
			}
		}
	}
}
