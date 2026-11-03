package controllers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/background"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/services/tusupload"
	"ch/kirari04/videocms/sharing"
	"github.com/google/uuid"
)

func (f *folderShareFixture) tusRequest(method, target, token, body string, headers map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	if strings.HasPrefix(target, "http") {
		u, _ := url.Parse(target)
		target = u.RequestURI()
	}
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Tus-Resumable", "1.0.0")
	req.Header.Set(tusupload.FolderShareHeader, token)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	f.e.ServeHTTP(rec, req)
	return rec
}

func sharedTusMetadata(name string, folderID uint, group string, size ...int) string {
	metadata := "filename " + base64.StdEncoding.EncodeToString([]byte(name)) + ",parent_folder_id " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprint(folderID))) + ",client_upload_uuid " + base64.StdEncoding.EncodeToString([]byte(group))
	if len(size) > 0 {
		metadata += ",file_size " + base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(size[0])))
	}
	return metadata
}

func (f *folderShareFixture) createGuestUpload(share models.FolderShare, folderID uint, group string, size int, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	return f.tusRequest(http.MethodPost, "/api/uploads", share.Token, "", map[string]string{
		"Upload-Length": strconv.Itoa(size), "Upload-Metadata": sharedTusMetadata("guest.mp4", folderID, group),
	}, cookies...)
}

func (f *folderShareFixture) completeGuestUpload(t *testing.T, share models.FolderShare, location, body string, cookies ...*http.Cookie) models.UploadSession {
	t.Helper()
	rec := f.tusRequest(http.MethodPatch, location, share.Token, body, map[string]string{"Content-Type": "application/offset+octet-stream", "Upload-Offset": "0"}, cookies...)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	id := path.Base(location)
	var session models.UploadSession
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := f.h.Deps.DB.Where("tus_id = ?", id).First(&session).Error; err == nil && session.Status == models.UploadStatusUploaded {
			return session
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("upload did not complete: %+v", session)
	return session
}

func TestGuestTusUploadResumesChunksAndQueuesOwnerPipeline(t *testing.T) {
	f := newFolderShareFixture(t)
	share := f.create(t, false, true, "")
	group := uuid.NewString()
	if rec := f.createGuestUpload(share, f.outside.ID, group, 11); rec.Code != 403 {
		t.Fatalf("escaped destination: %d", rec.Code)
	}
	create := f.createGuestUpload(share, f.root.ID, group, 11)
	if create.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", create.Code, create.Body.String())
	}
	location := create.Header().Get("Location")
	first := f.tusRequest(http.MethodPatch, location, share.Token, "guest", map[string]string{"Content-Type": "application/offset+octet-stream", "Upload-Offset": "0"})
	if first.Code != 204 {
		t.Fatalf("first chunk: %d", first.Code)
	}
	head := f.tusRequest(http.MethodHead, location, share.Token, "", nil)
	if head.Code != 200 || head.Header().Get("Upload-Offset") != "5" {
		t.Fatalf("resume offset: %d %v", head.Code, head.Header())
	}
	last := f.tusRequest(http.MethodPatch, location, share.Token, " video", map[string]string{"Content-Type": "application/offset+octet-stream", "Upload-Offset": "5"})
	if last.Code != 204 {
		t.Fatalf("resume: %d %s", last.Code, last.Body.String())
	}
	var session models.UploadSession
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.h.Deps.DB.Where("tus_id = ?", path.Base(location)).First(&session)
		if session.Status == models.UploadStatusUploaded {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	finalize := "/api/shares/" + share.Token + "/uploads/" + session.TusID + "/finalize"
	for attempt := 0; attempt < 2; attempt++ {
		rec := f.request(http.MethodPost, finalize, "", 0)
		if rec.Code != 202 || strings.Contains(rec.Body.String(), "UUID") || strings.Contains(rec.Body.String(), "job") || rec.Header().Get("Location") != "" {
			t.Fatalf("receipt exposed owner data: %d %s", rec.Code, rec.Body.String())
		}
	}
	f.h.Deps.DB.First(&session, session.ID)
	if session.UserID != f.owner.ID || session.ParentFolderID != f.root.ID || session.FolderShareID != share.ID || session.FolderShareVersion != share.AccessVersion || session.BackgroundJobID == "" {
		t.Fatalf("incorrect owner attribution: %+v", session)
	}
	data, err := os.ReadFile(session.StoragePath)
	if err != nil || string(data) != "guest video" {
		t.Fatalf("chunk assembly: %q %v", data, err)
	}
	var jobs []background.Job
	if err := f.h.Deps.DB.Find(&jobs).Error; err != nil || len(jobs) != 1 || jobs[0].OwnerID == nil || *jobs[0].OwnerID != f.owner.ID {
		t.Fatalf("duplicate or incorrectly owned ingestion: %+v %v", jobs, err)
	}
	f.h.Deps.DB.Model(&f.owner).Update("storage", 5)
	if rec := f.createGuestUpload(share, f.root.ID, uuid.NewString(), 11); rec.Code != 403 {
		t.Fatalf("owner quota bypassed: %d", rec.Code)
	}
}

func TestGuestTusUploadTargetsBrowsedSubfolder(t *testing.T) {
	for _, concatenate := range []bool{false, true} {
		t.Run(fmt.Sprintf("concatenate=%t", concatenate), func(t *testing.T) {
			f := newFolderShareFixture(t)
			share := f.create(t, true, true, "")
			browse := f.request(http.MethodGet, fmt.Sprintf("/api/shares/%s?FolderID=%d", share.Token, f.child.ID), "", 0)
			var folder sharedFolderResponse
			if err := json.Unmarshal(browse.Body.Bytes(), &folder); err != nil || browse.Code != 200 || folder.FolderID != f.child.ID {
				t.Fatalf("browse subfolder: %d %s %v", browse.Code, browse.Body.String(), err)
			}
			group := uuid.NewString()
			contents := []string{"guest"}
			if concatenate {
				contents = []string{"gu", "est"}
			}
			var locations []string
			for _, content := range contents {
				headers := map[string]string{"Upload-Length": strconv.Itoa(len(content)), "Upload-Metadata": sharedTusMetadata("guest.mp4", folder.FolderID, group, 5)}
				if concatenate {
					headers["Upload-Concat"] = "partial"
				}
				create := f.tusRequest(http.MethodPost, "/api/uploads", share.Token, "", headers)
				if create.Code != 201 {
					t.Fatalf("create in subfolder: %d %s", create.Code, create.Body.String())
				}
				location := create.Header().Get("Location")
				session := f.completeGuestUpload(t, share, location, content)
				if session.ParentFolderID != f.child.ID {
					t.Fatalf("chunk upload used folder %d instead of %d", session.ParentFolderID, f.child.ID)
				}
				locations = append(locations, location)
			}
			location := locations[0]
			if concatenate {
				final := f.tusRequest(http.MethodPost, "/api/uploads", share.Token, "", map[string]string{
					"Upload-Concat": "final;" + strings.Join(locations, " "), "Upload-Metadata": sharedTusMetadata("guest.mp4", folder.FolderID, group, 5),
				})
				if final.Code != 201 {
					t.Fatalf("concatenate in subfolder: %d %s", final.Code, final.Body.String())
				}
				location = final.Header().Get("Location")
			}
			var session models.UploadSession
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				f.h.Deps.DB.Where("tus_id = ?", path.Base(location)).First(&session)
				if session.Status == models.UploadStatusUploaded {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			receipt := f.request(http.MethodPost, "/api/shares/"+share.Token+"/uploads/"+session.TusID+"/finalize", "", 0)
			f.h.Deps.DB.First(&session, session.ID)
			if receipt.Code != 202 || session.ParentFolderID != f.child.ID || session.BackgroundJobID == "" {
				t.Fatalf("subfolder destination lost at finalization: %d %+v", receipt.Code, session)
			}
		})
	}
}

func TestGuestTusUploadsRecheckLiveAccessOnChunksAndFinalize(t *testing.T) {
	for _, change := range []string{"revoke", "expire", "settings", "remove upload", "move", "delete root", "delete owner", "disable uploads"} {
		t.Run(change, func(t *testing.T) {
			f := newFolderShareFixture(t)
			share := f.create(t, true, true, "")
			create := f.createGuestUpload(share, f.child.ID, uuid.NewString(), 11)
			if create.Code != 201 {
				t.Fatalf("create: %d", create.Code)
			}
			location := create.Header().Get("Location")
			session := f.completeGuestUpload(t, share, location, "guest video")
			switch change {
			case "revoke":
				f.h.Deps.DB.Delete(&share)
			case "expire":
				f.h.Deps.DB.Model(&share).Update("expires_at", time.Now().Add(-time.Minute))
			case "settings":
				f.h.Deps.DB.Model(&share).Update("access_version", uuid.NewString())
			case "remove upload":
				f.h.Deps.DB.Model(&share).Update("allow_upload", false)
			case "move":
				f.h.Deps.DB.Model(&f.child).Update("parent_folder_id", f.outside.ID)
			case "delete root":
				f.h.Deps.DB.Delete(&f.root)
			case "delete owner":
				f.h.Deps.DB.Delete(&f.owner)
			case "disable uploads":
				snapshot := f.h.Deps.Snapshots.Current()
				disabled := false
				snapshot.Config.UploadEnabled = &disabled
				f.h.Deps.Snapshots.Replace(snapshot)
			}
			for _, method := range []string{http.MethodHead, http.MethodPatch, http.MethodDelete} {
				rec := f.tusRequest(method, location, share.Token, "", nil)
				if rec.Code != 403 && rec.Code != 404 {
					t.Fatalf("stale %s accepted: %d %s", method, rec.Code, rec.Body.String())
				}
			}
			rec := f.request(http.MethodPost, "/api/shares/"+share.Token+"/uploads/"+session.TusID+"/finalize", "", 0)
			if rec.Code != 403 && rec.Code != 404 {
				t.Fatalf("stale finalize accepted: %d %s", rec.Code, rec.Body.String())
			}
			var count int64
			f.h.Deps.DB.Model(&background.Job{}).Count(&count)
			if count != 0 {
				t.Fatal("revoked upload queued ingestion")
			}
		})
	}
}

func TestGuestTusRequiresVisitorUnlockAndRejectsInvalidMetadata(t *testing.T) {
	f := newFolderShareFixture(t)
	share := f.create(t, false, true, "secret")
	if rec := f.createGuestUpload(share, f.root.ID, uuid.NewString(), 5); rec.Code != 401 {
		t.Fatalf("locked upload: %d", rec.Code)
	}
	token, _, err := f.h.Auth.GenerateFolderShareToken(&share)
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: auth.FolderShareCookieName(share.Token), Value: token}
	create := f.createGuestUpload(share, f.root.ID, uuid.NewString(), 5, cookie)
	if create.Code != 201 {
		t.Fatalf("unlocked upload: %d %s", create.Code, create.Body.String())
	}
	if rec := f.tusRequest(http.MethodHead, create.Header().Get("Location"), share.Token, "", nil); rec.Code != 401 {
		t.Fatalf("cookie missing on resume: %d", rec.Code)
	}
	for _, test := range []struct {
		name   string
		folder uint
		size   string
		want   int
	}{
		{"document.txt", f.root.ID, "5", 400}, {"guest.mp4", f.child.ID, "5", 403}, {"guest.mp4", f.root.ID, "1025", 413},
	} {
		rec := f.tusRequest(http.MethodPost, "/api/uploads", share.Token, "", map[string]string{"Upload-Length": test.size, "Upload-Metadata": sharedTusMetadata(test.name, test.folder, uuid.NewString())}, cookie)
		if rec.Code != test.want {
			t.Fatalf("metadata %s: %d %s", test.name, rec.Code, rec.Body.String())
		}
	}
}

func TestGuestTusCannotAccessAnotherShareOrOwnerSession(t *testing.T) {
	f := newFolderShareFixture(t)
	a := f.create(t, true, true, "")
	b := f.create(t, true, true, "")
	group := uuid.NewString()
	create := f.createGuestUpload(a, f.root.ID, group, 5)
	if create.Code != 201 {
		t.Fatalf("create: %d", create.Code)
	}
	location := create.Header().Get("Location")
	for _, method := range []string{http.MethodHead, http.MethodPatch, http.MethodDelete} {
		if rec := f.tusRequest(method, location, b.Token, "", nil); rec.Code != 403 {
			t.Fatalf("other share %s: %d", method, rec.Code)
		}
	}
	if rec := f.createGuestUpload(b, f.root.ID, group, 5); rec.Code != 403 {
		t.Fatalf("mixed share group: %d", rec.Code)
	}
	ownerSession := models.UploadSession{UserID: f.owner.ID, TusID: uuid.NewString(), ClientUploadUUID: uuid.NewString(), Protocol: models.UploadProtocolTus, Kind: models.UploadKindSingle, ParentFolderID: f.root.ID}
	if err := f.h.Deps.DB.Create(&ownerSession).Error; err != nil {
		t.Fatal(err)
	}
	if rec := f.tusRequest(http.MethodHead, "/api/uploads/"+ownerSession.TusID, a.Token, "", nil); rec.Code != 403 {
		t.Fatalf("owner upload exposed: %d", rec.Code)
	}
	if rec := f.createGuestUpload(a, f.root.ID, ownerSession.ClientUploadUUID, 5); rec.Code != 403 {
		t.Fatalf("joined owner group: %d", rec.Code)
	}
}

func TestGuestTusConcatenationAndCancellation(t *testing.T) {
	f := newFolderShareFixture(t)
	share := f.create(t, true, true, "")
	f.h.Deps.DB.Model(&share).Update("max_folder_size", 16)
	group := uuid.NewString()
	var partials []string
	for _, content := range []string{"guest", " video"} {
		create := f.tusRequest(http.MethodPost, "/api/uploads", share.Token, "", map[string]string{
			"Upload-Length": strconv.Itoa(len(content)), "Upload-Concat": "partial", "Upload-Metadata": sharedTusMetadata("guest.mp4", f.root.ID, group, 11),
		})
		if create.Code != 201 {
			t.Fatalf("partial create: %d %s", create.Code, create.Body.String())
		}
		location := create.Header().Get("Location")
		f.completeGuestUpload(t, share, location, content)
		partials = append(partials, location)
	}
	for _, folderID := range []uint{f.child.ID, f.root.ID} {
		name := "guest.mp4"
		if folderID == f.root.ID {
			name = "different.mp4"
		}
		rec := f.tusRequest(http.MethodPost, "/api/uploads", share.Token, "", map[string]string{"Upload-Concat": "final;" + strings.Join(partials, " "), "Upload-Metadata": sharedTusMetadata(name, folderID, group)})
		if rec.Code != 403 {
			t.Fatalf("concatenation changed destination or filename: %d %s", rec.Code, rec.Body.String())
		}
	}
	final := f.tusRequest(http.MethodPost, "/api/uploads", share.Token, "", map[string]string{"Upload-Concat": "final;" + strings.Join(partials, " "), "Upload-Metadata": sharedTusMetadata("guest.mp4", f.root.ID, group)})
	if final.Code != 201 {
		t.Fatalf("concatenation: %d %s", final.Code, final.Body.String())
	}
	var session models.UploadSession
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.h.Deps.DB.Where("tus_id = ?", path.Base(final.Header().Get("Location"))).First(&session)
		if session.Status == models.UploadStatusUploaded {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	data, err := os.ReadFile(session.StoragePath)
	if err != nil || string(data) != "guest video" || session.Kind != models.UploadKindFinal || session.Size != 11 {
		t.Fatalf("assembled guest upload: %q %+v %v", data, session, err)
	}
	receipt := f.request(http.MethodPost, "/api/shares/"+share.Token+"/uploads/"+session.TusID+"/finalize", "", 0)
	if receipt.Code != 202 {
		t.Fatalf("final receipt: %d %s", receipt.Code, receipt.Body.String())
	}
	// A completed concatenation reserves the whole video once, not each part
	// plus another copy for the final resource.
	share.MaxFolderSize = 16
	capacity, err := sharing.UploadCapacity(f.h.Deps.DB, &share, "")
	if err != nil || capacity.Used != 5 || capacity.Reserved != 11 {
		t.Fatalf("concatenation capacity: %+v %v", capacity, err)
	}
	f.h.Deps.DB.Model(&share).Update("max_folder_size", 0)
	cancel := f.createGuestUpload(share, f.root.ID, uuid.NewString(), 5)
	location := cancel.Header().Get("Location")
	if rec := f.tusRequest(http.MethodDelete, location, share.Token, "", nil); rec.Code != 204 {
		t.Fatalf("cancel: %d", rec.Code)
	}
	if rec := f.tusRequest(http.MethodHead, location, share.Token, "", nil); rec.Code != 404 {
		t.Fatalf("cancelled resource resumed: %d", rec.Code)
	}
}
