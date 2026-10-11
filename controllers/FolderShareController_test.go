package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/background"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/middlewares"
	"ch/kirari04/videocms/models"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type folderShareFixture struct {
	h                    *Handlers
	e                    *echo.Echo
	owner                models.User
	root, child, outside models.Folder
	link                 models.Link
}

func newFolderShareFixture(t *testing.T) *folderShareFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableForeignKeyConstraintWhenMigrating: true, IgnoreRelationshipsWhenMigrating: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.User{}, &models.Folder{}, &models.FolderShare{}, &models.File{}, &models.Link{}, &models.Quality{}, &models.Tag{}, &models.TagLinks{}, &models.UploadSession{}, &models.UploadLog{}); err != nil {
		t.Fatal(err)
	}
	if err := background.Migrate(db); err != nil {
		t.Fatal(err)
	}
	enabled, disabled := true, false
	deps := &app.Deps{DB: db, RequestGate: app.NewRequestGate(), Background: background.New(db, background.Options{}), Snapshots: app.NewSnapshotStore(app.Snapshot{Config: config.Config{
		JwtSecretKey: "share-test-secret", JwtMediaSecretKey: "media-test-secret", UploadEnabled: &enabled, CaptchaEnabled: &disabled,
		FolderVideoUploadsPriv: t.TempDir(), FolderVideoQualitysPub: "/videos/qualitys", MaxUploadFilesize: 1024, MaxUploadSessions: 10,
	}})}
	f := &folderShareFixture{h: NewHandlers(deps, auth.NewService(deps), logic.NewService(deps), nil, nil), e: echo.New()}
	f.owner = models.User{Username: "owner", Email: "private@example.test", Storage: 1024, Settings: models.UserSettings{UploadSessionsMax: 10}}
	for _, value := range []any{&f.owner} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	f.root = models.Folder{Name: "Shared videos", UserID: f.owner.ID}
	if err := db.Create(&f.root).Error; err != nil {
		t.Fatal(err)
	}
	f.child = models.Folder{Name: "Nested", UserID: f.owner.ID, ParentFolderID: f.root.ID}
	f.outside = models.Folder{Name: "Private folder", UserID: f.owner.ID}
	for _, value := range []any{&f.child, &f.outside} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	file := models.File{UUID: uuid.NewString(), UserID: f.owner.ID, Size: 5}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	f.link = models.Link{UUID: uuid.NewString(), UserID: f.owner.ID, ParentFolderID: f.child.ID, FileID: file.ID, Name: "Private video name"}
	if err := db.Create(&f.link).Error; err != nil {
		t.Fatal(err)
	}
	ownerAuth := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			id, _ := strconv.ParseUint(c.Request().Header.Get("X-Test-Owner"), 10, 64)
			c.Set("UserID", uint(id))
			return next(c)
		}
	}
	ownerRoutes := f.e.Group("/api/folder-shares", ownerAuth)
	ownerRoutes.GET("", f.h.ListFolderShares)
	ownerRoutes.POST("", f.h.CreateFolderShare)
	ownerRoutes.PUT("/:id", f.h.UpdateFolderShare)
	ownerRoutes.DELETE("/:id", f.h.DeleteFolderShare)
	f.e.GET("/api/shares/:token", f.h.GetSharedFolder)
	f.e.POST("/api/shares/:token/unlock", f.h.UnlockFolderShare)
	f.e.POST("/api/shares/:token/uploads", f.h.UploadToSharedFolder)
	f.e.GET("/share-videos/:token/:UUID", f.h.SharedVideo(func(c echo.Context) error { return c.NoContent(http.StatusOK) }))
	f.e.GET("/media/:UUID", func(c echo.Context) error { return c.NoContent(http.StatusOK) }, middlewares.NewFactory(deps, f.h.Auth).MediaAuth())
	return f
}

func (f *folderShareFixture) request(method, path, body string, owner uint, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", echo.MIMEApplicationJSON)
	req.Header.Set("X-Test-Owner", fmt.Sprint(owner))
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	f.e.ServeHTTP(rec, req)
	return rec
}

func (f *folderShareFixture) create(t *testing.T, read, upload bool, password string) models.FolderShare {
	t.Helper()
	body, _ := json.Marshal(models.FolderShareWrite{FolderID: f.root.ID, AllowRead: read, AllowUpload: upload, Password: &password})
	rec := f.request(http.MethodPost, "/api/folder-shares", string(body), f.owner.ID)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var response folderShareResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	var share models.FolderShare
	if err := f.h.Deps.DB.First(&share, response.ID).Error; err != nil {
		t.Fatal(err)
	}
	return share
}

func TestFolderShareOwnerControlsAndPasswordStorage(t *testing.T) {
	f := newFolderShareFixture(t)
	share := f.create(t, true, false, "simple password")
	if len(share.Token) != 43 || share.PasswordHash == "simple password" || bcrypt.CompareHashAndPassword([]byte(share.PasswordHash), []byte("simple password")) != nil {
		t.Fatal("share token or password storage is invalid")
	}
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		rec := f.request(method, fmt.Sprintf("/api/folder-shares/%d", share.ID), `{}`, f.owner.ID+1)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("other owner %s: %d", method, rec.Code)
		}
	}
	rec := f.request(http.MethodGet, fmt.Sprintf("/api/folder-shares?FolderID=%d", f.root.ID), "", f.owner.ID)
	if strings.Contains(rec.Body.String(), "PasswordHash") || strings.Contains(rec.Body.String(), "AccessVersion") || strings.Contains(rec.Body.String(), "simple password") {
		t.Fatal("owner response exposed credentials")
	}
	noPermissions := fmt.Sprintf(`{"FolderID":%d}`, f.root.ID)
	if rec := f.request(http.MethodPost, "/api/folder-shares", noPermissions, f.owner.ID); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty permissions: %d", rec.Code)
	}
	if rec := f.request(http.MethodPost, "/api/folder-shares", fmt.Sprintf(`{"FolderID":%d,"AllowRead":true}`, f.root.ID), f.owner.ID+1); rec.Code != http.StatusNotFound {
		t.Fatalf("shared another owner's folder: %d", rec.Code)
	}
}

func TestSharedFolderReadsAreLimitedToCurrentOwnedSubtree(t *testing.T) {
	f := newFolderShareFixture(t)
	share := f.create(t, true, true, "")
	base := "/api/shares/" + share.Token
	rec := f.request(http.MethodGet, base, "", 0)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Nested") || strings.Contains(rec.Body.String(), f.owner.Email) {
		t.Fatalf("root response: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.request(http.MethodGet, fmt.Sprintf("%s?FolderID=%d", base, f.child.ID), "", 0)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "/share-videos/") || !strings.Contains(rec.Body.String(), f.link.Name) {
		t.Fatalf("child response: %d %s", rec.Code, rec.Body.String())
	}
	for _, id := range []uint{0, f.outside.ID} {
		if rec := f.request(http.MethodGet, fmt.Sprintf("%s?FolderID=%d", base, id), "", 0); rec.Code != 403 {
			t.Fatalf("escaped subtree %d: %d", id, rec.Code)
		}
	}
	if err := f.h.Deps.DB.Model(&f.child).Update("parent_folder_id", f.outside.ID).Error; err != nil {
		t.Fatal(err)
	}
	if rec := f.request(http.MethodGet, fmt.Sprintf("%s?FolderID=%d", base, f.child.ID), "", 0); rec.Code != 403 {
		t.Fatalf("moved folder remained accessible: %d", rec.Code)
	}
}

func TestUploadOnlyShareHidesContentsAndRejectsPlayback(t *testing.T) {
	f := newFolderShareFixture(t)
	share := f.create(t, false, true, "")
	rec := f.request(http.MethodGet, "/api/shares/"+share.Token, "", 0)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), f.child.Name) || strings.Contains(rec.Body.String(), f.link.Name) || strings.Contains(rec.Body.String(), f.link.UUID) {
		t.Fatalf("upload-only content leak: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.request(http.MethodGet, "/share-videos/"+share.Token+"/"+f.link.UUID, "", 0); rec.Code != 403 {
		t.Fatalf("upload-only playback: %d", rec.Code)
	}
	if rec := f.request(http.MethodGet, fmt.Sprintf("/api/shares/%s?FolderID=%d", share.Token, f.child.ID), "", 0); rec.Code != 403 {
		t.Fatalf("upload-only subfolder: %d", rec.Code)
	}
}

func TestSharePasswordSessionsInvalidateOnSettingsChange(t *testing.T) {
	f := newFolderShareFixture(t)
	share := f.create(t, true, true, "secret")
	base := "/api/shares/" + share.Token
	rec := f.request(http.MethodGet, base, "", 0)
	if !strings.Contains(rec.Body.String(), `"Locked":true`) || strings.Contains(rec.Body.String(), f.root.Name) {
		t.Fatal("locked folder leaked content")
	}
	if rec := f.request(http.MethodPost, base+"/uploads", "", 0); rec.Code != http.StatusUnauthorized {
		t.Fatalf("locked share accepted an upload: %d", rec.Code)
	}
	unlock := func(password string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, base+"/unlock", strings.NewReader(url.Values{"password": {password}}.Encode()))
		req.Header.Set("Content-Type", echo.MIMEApplicationForm)
		rec := httptest.NewRecorder()
		f.e.ServeHTTP(rec, req)
		return rec
	}
	if rec := unlock("wrong"); rec.Code != 401 {
		t.Fatalf("wrong password: %d", rec.Code)
	}
	rec = unlock("secret")
	if rec.Code != 204 {
		t.Fatalf("unlock: %d %s", rec.Code, rec.Body.String())
	}
	cookie := rec.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("unsafe share cookie")
	}
	if rec := f.request(http.MethodGet, base, "", 0, cookie); !strings.Contains(rec.Body.String(), `"Locked":false`) {
		t.Fatal("password did not unlock folder")
	}
	if rec := f.request(http.MethodGet, "/share-videos/"+share.Token+"/"+f.link.UUID, "", 0, cookie); rec.Code != 200 {
		t.Fatalf("shared playback: %d", rec.Code)
	}
	// The same session cannot unlock a different share.
	other := f.create(t, true, false, "secret")
	if rec := f.request(http.MethodGet, "/api/shares/"+other.Token, "", 0, cookie); !strings.Contains(rec.Body.String(), `"Locked":true`) {
		t.Fatal("session unlocked another share")
	}
	change := fmt.Sprintf(`{"FolderID":%d,"AllowRead":true,"AllowUpload":false}`, f.root.ID)
	if rec := f.request(http.MethodPut, fmt.Sprintf("/api/folder-shares/%d", share.ID), change, f.owner.ID); rec.Code != 200 {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.request(http.MethodGet, base, "", 0, cookie); !strings.Contains(rec.Body.String(), `"Locked":true`) {
		t.Fatal("settings change did not end session")
	}
	var updated models.FolderShare
	f.h.Deps.DB.First(&updated, share.ID)
	if updated.PasswordHash != share.PasswordHash {
		t.Fatal("omitted password removed protection")
	}
}

func TestSharedMediaChecksRevocationPermissionsAndFolderMoves(t *testing.T) {
	for _, change := range []string{"revoke", "remove read", "version", "move", "delete root", "delete owner", "expire"} {
		t.Run(change, func(t *testing.T) {
			f := newFolderShareFixture(t)
			share := f.create(t, true, false, "")
			media, _, err := f.h.Auth.GenerateMediaToken(auth.MediaClaims{LinkUUID: f.link.UUID, ShareToken: share.Token, ShareVersion: share.AccessVersion})
			if err != nil {
				t.Fatal(err)
			}
			cookie := &http.Cookie{Name: auth.MediaCookieName, Value: media}
			if rec := f.request(http.MethodGet, "/media/"+f.link.UUID, "", 0, cookie); rec.Code != 200 {
				t.Fatalf("initial media: %d", rec.Code)
			}
			switch change {
			case "revoke":
				f.h.Deps.DB.Delete(&share)
			case "remove read":
				f.h.Deps.DB.Model(&share).Update("allow_read", false)
			case "version":
				f.h.Deps.DB.Model(&share).Update("access_version", uuid.NewString())
			case "move":
				f.h.Deps.DB.Model(&f.child).Update("parent_folder_id", f.outside.ID)
			case "delete root":
				f.h.Deps.DB.Delete(&f.root)
			case "delete owner":
				f.h.Deps.DB.Delete(&f.owner)
			case "expire":
				f.h.Deps.DB.Model(&share).Update("expires_at", time.Now().Add(-time.Minute))
			}
			if rec := f.request(http.MethodGet, "/media/"+f.link.UUID, "", 0, cookie); rec.Code != 403 {
				t.Fatalf("stale media accepted: %d", rec.Code)
			}
		})
	}
}

func TestGuestUploadUsesOwnerPipelineWithoutReturningOwnerDetails(t *testing.T) {
	f := newFolderShareFixture(t)
	share := f.create(t, false, true, "")
	upload := func(query string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("file", "guest.mp4")
		_, _ = part.Write([]byte("guest video"))
		_ = writer.WriteField("ParentFolderID", fmt.Sprint(f.outside.ID))
		_ = writer.WriteField("Name", "Guest video")
		_ = writer.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/shares/"+share.Token+"/uploads"+query, &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		f.e.ServeHTTP(rec, req)
		return rec
	}
	if rec := upload(fmt.Sprintf("?FolderID=%d", f.outside.ID)); rec.Code != 403 {
		t.Fatalf("upload escaped folder: %d", rec.Code)
	}
	rec := upload("")
	if rec.Code != 202 || strings.Contains(rec.Body.String(), "UUID") || rec.Header().Get("Location") != "" {
		t.Fatalf("upload receipt: %d %s", rec.Code, rec.Body.String())
	}
	var session models.UploadSession
	if err := f.h.Deps.DB.First(&session).Error; err != nil {
		t.Fatal(err)
	}
	if session.UserID != f.owner.ID || session.ParentFolderID != f.root.ID || session.FolderShareID != share.ID || session.BackgroundJobID == "" || session.Name != "Guest video.mp4" {
		t.Fatalf("incorrect upload attribution: %+v", session)
	}
	if bytes, err := os.ReadFile(session.StoragePath); err != nil || string(bytes) != "guest video" {
		t.Fatalf("staging failed: %q %v", bytes, err)
	}
	f.h.Deps.DB.Model(&f.owner).Update("storage", 5)
	if rec := upload(""); rec.Code != 403 {
		t.Fatalf("owner quota bypassed: %d %s", rec.Code, rec.Body.String())
	}
	f.h.Deps.DB.Model(&share).Update("allow_upload", false)
	if rec := upload(""); rec.Code != 403 {
		t.Fatalf("read-only upload: %d", rec.Code)
	}
}

func TestShareCaptchaFailsClosedWhenServerCaptchaIsDisabled(t *testing.T) {
	f := newFolderShareFixture(t)
	body := fmt.Sprintf(`{"FolderID":%d,"AllowRead":true,"RequireCaptcha":true}`, f.root.ID)
	if rec := f.request(http.MethodPost, "/api/folder-shares", body, f.owner.ID); rec.Code != 400 {
		t.Fatalf("unconfigured captcha enabled: %d", rec.Code)
	}
	share := f.create(t, true, false, "")
	f.h.Deps.DB.Model(&share).Update("require_captcha", true)
	if rec := f.request(http.MethodPost, "/api/shares/"+share.Token+"/unlock", "", 0); rec.Code != 503 {
		t.Fatalf("disabled captcha bypass: %d", rec.Code)
	}
	remove := fmt.Sprintf(`{"FolderID":%d,"AllowRead":true,"RequireCaptcha":false}`, f.root.ID)
	if rec := f.request(http.MethodPut, fmt.Sprintf("/api/folder-shares/%d", share.ID), remove, f.owner.ID); rec.Code != http.StatusOK {
		t.Fatalf("owner could not remove unavailable CAPTCHA: %d %s", rec.Code, rec.Body.String())
	}
}

// Change the share only when multipart parsing starts, after the handler has
// checked the initial authorization. No timing assumptions or goroutines are
// needed to reproduce settings changes during a slow request body.
type sharedUploadChangingBody struct {
	io.Reader
	onRead func()
}

func (b *sharedUploadChangingBody) Read(p []byte) (int, error) {
	if b.onRead != nil {
		change := b.onRead
		b.onRead = nil
		change()
	}
	return b.Reader.Read(p)
}

func TestSharedUploadRechecksAccessAfterReceivingBody(t *testing.T) {
	for _, change := range []string{"revoke", "expire", "settings", "remove upload", "move", "delete root", "delete owner", "disable uploads"} {
		t.Run(change, func(t *testing.T) {
			f := newFolderShareFixture(t)
			share := f.create(t, true, true, "")
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, _ := writer.CreateFormFile("file", "guest.mp4")
			_, _ = part.Write([]byte("guest video"))
			_ = writer.Close()
			changingBody := &sharedUploadChangingBody{Reader: &body, onRead: func() {
				var err error
				switch change {
				case "revoke":
					err = f.h.Deps.DB.Delete(&share).Error
				case "expire":
					err = f.h.Deps.DB.Model(&share).Update("expires_at", time.Now().Add(-time.Minute)).Error
				case "settings":
					err = f.h.Deps.DB.Model(&share).Update("access_version", uuid.NewString()).Error
				case "remove upload":
					err = f.h.Deps.DB.Model(&share).Update("allow_upload", false).Error
				case "move":
					err = f.h.Deps.DB.Model(&f.child).Update("parent_folder_id", f.outside.ID).Error
				case "delete root":
					err = f.h.Deps.DB.Delete(&f.root).Error
				case "delete owner":
					err = f.h.Deps.DB.Delete(&f.owner).Error
				case "disable uploads":
					snapshot := f.h.Deps.Snapshots.Current()
					disabled := false
					snapshot.Config.UploadEnabled = &disabled
					f.h.Deps.Snapshots.Replace(snapshot)
				}
				if err != nil {
					t.Fatal(err)
				}
			}}
			req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/shares/%s/uploads?FolderID=%d", share.Token, f.child.ID), changingBody)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			rec := httptest.NewRecorder()
			f.e.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
				t.Fatalf("changed share accepted body: %d %s", rec.Code, rec.Body.String())
			}
			var count int64
			if err := f.h.Deps.DB.Model(&models.UploadSession{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("rejected upload consumed a session: count=%d err=%v", count, err)
			}
			files, err := os.ReadDir(f.h.Config().FolderVideoUploadsPriv)
			if err != nil || len(files) != 0 {
				t.Fatalf("rejected upload left staged data: files=%v err=%v", files, err)
			}
		})
	}
}

func TestProtectedSharedMediaRequiresCurrentVisitorSession(t *testing.T) {
	f := newFolderShareFixture(t)
	share := f.create(t, true, false, "secret")
	mediaToken, _, err := f.h.Auth.GenerateMediaToken(auth.MediaClaims{LinkUUID: f.link.UUID, ShareToken: share.Token, ShareVersion: share.AccessVersion})
	if err != nil {
		t.Fatal(err)
	}
	media := &http.Cookie{Name: auth.MediaCookieName, Value: mediaToken}
	path := "/media/" + f.link.UUID
	if rec := f.request(http.MethodGet, path, "", 0, media); rec.Code != http.StatusUnauthorized {
		t.Fatalf("media token bypassed share session: %d", rec.Code)
	}
	sessionToken, _, err := f.h.Auth.GenerateFolderShareToken(&share)
	if err != nil {
		t.Fatal(err)
	}
	session := &http.Cookie{Name: auth.FolderShareCookieName(share.Token), Value: sessionToken}
	if rec := f.request(http.MethodGet, path, "", 0, media, session); rec.Code != http.StatusOK {
		t.Fatalf("valid protected media rejected: %d", rec.Code)
	}
	session.Value += "invalid"
	if rec := f.request(http.MethodGet, path, "", 0, media, session); rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid visitor session accepted: %d", rec.Code)
	}
}

func TestSharedFolderRejectsUnsupportedVideoBeforeStaging(t *testing.T) {
	f := newFolderShareFixture(t)
	share := f.create(t, false, true, "")
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "document.txt")
	_, _ = part.Write([]byte("not a video"))
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/shares/"+share.Token+"/uploads", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	f.e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported file accepted: %d", rec.Code)
	}
	var count int64
	f.h.Deps.DB.Model(&models.UploadSession{}).Count(&count)
	if count != 0 {
		t.Fatal("unsupported file consumed an upload session")
	}
}
