package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/sharing"
	"github.com/google/uuid"
)

func TestFolderShareUploadSettingsDefaultsAndPersistence(t *testing.T) {
	f := newFolderShareFixture(t)
	share := f.create(t, false, true, "")
	if !share.ReturnsUploadLink() || share.MaxVideoSize != 0 || share.MaxFolderSize != 0 {
		t.Fatal("incorrect defaults")
	}
	target := fmt.Sprintf("/api/folder-shares/%d", share.ID)
	body := fmt.Sprintf(`{"FolderID":%d,"AllowUpload":true,"MaxVideoSize":7,"MaxFolderSize":20,"ReturnUploadLink":false}`, f.root.ID)
	rec := f.request(http.MethodPut, target, body, f.owner.ID)
	if rec.Code != 200 {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	// Older clients which omit these settings must preserve them.
	rec = f.request(http.MethodPut, target, fmt.Sprintf(`{"FolderID":%d,"AllowUpload":true}`, f.root.ID), f.owner.ID)
	var result folderShareResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || result.ReturnUploadLink || result.MaxVideoSize != 7 || result.MaxFolderSize != 20 {
		t.Fatalf("lost settings: %s %v", rec.Body.String(), err)
	}
	for _, field := range []string{"MaxVideoSize", "MaxFolderSize"} {
		rec := f.request(http.MethodPut, target, fmt.Sprintf(`{"FolderID":%d,"AllowUpload":true,"%s":-1}`, f.root.ID, field), f.owner.ID)
		if rec.Code != 400 {
			t.Fatalf("negative %s accepted: %d", field, rec.Code)
		}
	}
	// Explicit false must survive creation despite the database's true default.
	rec = f.request(http.MethodPost, "/api/folder-shares", body, f.owner.ID)
	if err := json.Unmarshal(rec.Body.Bytes(), &result); rec.Code != 201 || err != nil || result.ReturnUploadLink {
		t.Fatalf("false default overwritten: %d %s", rec.Code, rec.Body.String())
	}
}

func TestExistingFolderSharesDefaultToReturningLinksAfterMigration(t *testing.T) {
	f := newFolderShareFixture(t)
	share := f.create(t, false, true, "")
	if err := f.h.Deps.DB.Exec("ALTER TABLE folder_shares DROP COLUMN return_upload_link").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.h.Deps.DB.AutoMigrate(&models.FolderShare{}); err != nil {
		t.Fatal(err)
	}
	if err := f.h.Deps.DB.First(&share, share.ID).Error; err != nil || !share.ReturnsUploadLink() {
		t.Fatalf("legacy default: %+v %v", share, err)
	}
}

func TestGuestVideoLimitAppliesToWholeChunkedVideo(t *testing.T) {
	f := newFolderShareFixture(t)
	share := f.create(t, true, true, "")
	f.h.Deps.DB.Model(&share).Update("max_video_size", 7)
	if rec := f.createGuestUpload(share, f.child.ID, uuid.NewString(), 8); rec.Code != 413 {
		t.Fatalf("single limit: %d %s", rec.Code, rec.Body.String())
	}
	for _, size := range []int{8, 2048} {
		rec := f.tusRequest(http.MethodPost, "/api/uploads", share.Token, "", map[string]string{"Upload-Length": "2", "Upload-Concat": "partial", "Upload-Metadata": sharedTusMetadata("guest.mp4", f.child.ID, uuid.NewString(), size)})
		if rec.Code != 413 {
			t.Fatalf("full partial size %d: %d %s", size, rec.Code, rec.Body.String())
		}
	}
	group := uuid.NewString()
	partial := func(length int, total ...int) int {
		return f.tusRequest(http.MethodPost, "/api/uploads", share.Token, "", map[string]string{"Upload-Length": fmt.Sprint(length), "Upload-Concat": "partial", "Upload-Metadata": sharedTusMetadata("guest.mp4", f.child.ID, group, total...)}).Code
	}
	if partial(2) != 400 || partial(2, 5) != 201 || partial(2, 6) != 400 || partial(4, 5) != 400 {
		t.Fatal("declared size could be omitted, changed, or exceeded")
	}
}

func TestGuestFolderCapacityIncludesSubtreeAndReservations(t *testing.T) {
	f := newFolderShareFixture(t)
	share := f.create(t, true, true, "")
	f.h.Deps.DB.Model(&share).Update("max_folder_size", 15)
	share.MaxFolderSize = 15 // Existing child video occupies five bytes.
	group := uuid.NewString()
	partial := f.tusRequest(http.MethodPost, "/api/uploads", share.Token, "", map[string]string{"Upload-Length": "2", "Upload-Concat": "partial", "Upload-Metadata": sharedTusMetadata("guest.mp4", f.child.ID, group, 10)})
	if partial.Code != 201 {
		t.Fatalf("reserve: %d %s", partial.Code, partial.Body.String())
	}
	capacity, err := sharing.UploadCapacity(f.h.Deps.DB, &share, "")
	if err != nil || capacity.Used != 5 || capacity.Reserved != 10 || capacity.Remaining != 0 {
		t.Fatalf("capacity: %+v %v", capacity, err)
	}
	if rec := f.createGuestUpload(share, f.root.ID, uuid.NewString(), 1); rec.Code != 507 {
		t.Fatalf("pending reservation bypassed: %d", rec.Code)
	}
	rest := f.tusRequest(http.MethodPost, "/api/uploads", share.Token, "", map[string]string{"Upload-Length": "8", "Upload-Concat": "partial", "Upload-Metadata": sharedTusMetadata("guest.mp4", f.child.ID, group, 10)})
	if rest.Code != 201 {
		t.Fatalf("reserved video cannot continue: %d %s", rest.Code, rest.Body.String())
	}
	for _, loc := range []string{partial.Header().Get("Location"), rest.Header().Get("Location")} {
		if rec := f.tusRequest(http.MethodDelete, loc, share.Token, "", nil); rec.Code != 204 {
			t.Fatalf("cancel: %d", rec.Code)
		}
	}
	// Termination events update the reservations asynchronously after DELETE.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		capacity, err = sharing.UploadCapacity(f.h.Deps.DB, &share, "")
		if err != nil {
			t.Fatalf("capacity after cancel: %v", err)
		}
		if capacity.Reserved == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if capacity.Reserved != 0 {
		t.Fatalf("cancel did not release reservation: %+v", capacity)
	}
	if rec := f.createGuestUpload(share, f.root.ID, uuid.NewString(), 10); rec.Code != 201 {
		t.Fatalf("cancel did not release reservation: %d %s", rec.Code, rec.Body.String())
	}
	var single models.UploadSession
	f.h.Deps.DB.Where("kind = ? AND status = ?", models.UploadKindSingle, models.UploadStatusCreated).First(&single)
	if rec := f.createGuestUpload(share, f.root.ID, single.ClientUploadUUID, 10); rec.Code != 409 {
		t.Fatalf("duplicate group overbooked capacity: %d", rec.Code)
	}
	// Expired uploads release space even before the cleanup worker runs.
	f.h.Deps.DB.Model(&models.UploadSession{}).Where("status IN ?", []string{models.UploadStatusCreated, models.UploadStatusUploading}).Update("expires_at", time.Now().Add(-time.Minute))
	if rec := f.createGuestUpload(share, f.child.ID, uuid.NewString(), 10); rec.Code != 201 {
		t.Fatalf("expiry did not release space: %d %s", rec.Code, rec.Body.String())
	}
}

func TestConcurrentGuestUploadsCannotOverbookFolder(t *testing.T) {
	f := newFolderShareFixture(t)
	share := f.create(t, true, true, "")
	f.h.Deps.DB.Model(&share).Update("max_folder_size", 15)
	results := make(chan int, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			results <- f.createGuestUpload(share, f.child.ID, uuid.NewString(), 10).Code
		}()
	}
	workers.Wait()
	close(results)
	counts := map[int]int{}
	for code := range results {
		counts[code]++
	}
	if counts[201] != 1 || counts[507] != 1 {
		t.Fatalf("overbooked: %v", counts)
	}
}

func TestImportedVideoReplacesReservationWithoutDoubleCounting(t *testing.T) {
	f := newFolderShareFixture(t)
	share := f.create(t, true, true, "")
	share.MaxFolderSize = 20
	f.h.Deps.DB.Model(&share).Update("max_folder_size", 20)
	group := uuid.NewString()
	create := f.createGuestUpload(share, f.child.ID, group, 10)
	if create.Code != 201 {
		t.Fatalf("create: %d", create.Code)
	}
	file := models.File{UUID: uuid.NewString(), UserID: f.owner.ID, Size: 10}
	f.h.Deps.DB.Create(&file)
	link := models.Link{UUID: uuid.NewString(), UserID: f.owner.ID, ParentFolderID: f.child.ID, FileID: file.ID, CreationKey: fmt.Sprintf("upload:%d:%s", f.owner.ID, group)}
	if err := f.h.Deps.DB.Create(&link).Error; err != nil {
		t.Fatal(err)
	}
	capacity, err := sharing.UploadCapacity(f.h.Deps.DB, &share, "")
	if err != nil || capacity.Used != 15 || capacity.Reserved != 0 || capacity.Remaining != 5 {
		t.Fatalf("double counted imported video: %+v %v", capacity, err)
	}
	if rec := f.createGuestUpload(share, f.root.ID, uuid.NewString(), 5); rec.Code != 201 {
		t.Fatalf("remaining space unavailable: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGuestPublicLinkStatusIsScopedAndOptional(t *testing.T) {
	f := newFolderShareFixture(t)
	share := f.create(t, true, true, "")
	create := f.createGuestUpload(share, f.child.ID, uuid.NewString(), 5)
	session := f.completeGuestUpload(t, share, create.Header().Get("Location"), "guest")
	target := "/api/shares/" + share.Token + "/uploads/" + path.Base(create.Header().Get("Location"))
	rec := f.request(http.MethodGet, target, "", 0)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"Status":"processing"`) || strings.Contains(rec.Body.String(), "PublicURL") {
		t.Fatalf("processing: %d %s", rec.Code, rec.Body.String())
	}
	f.h.Deps.DB.Model(&session).Updates(map[string]any{"status": models.UploadStatusDone, "link_id": f.link.ID})
	f.h.Deps.DB.Delete(&session) // Completed sessions are retained only as soft-deleted records.
	rec = f.request(http.MethodGet, target, "", 0)
	var result struct{ Status, PublicURL string }
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || rec.Code != 200 || result.Status != "ready" || result.PublicURL != "/v/"+f.link.UUID {
		t.Fatalf("ready: %d %s %v", rec.Code, rec.Body.String(), err)
	}
	if strings.Contains(rec.Body.String(), "Job") || strings.Contains(rec.Body.String(), "UserID") || strings.Contains(rec.Body.String(), "FileID") || !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("private status leaked")
	}
	other := f.create(t, true, true, "")
	if rec := f.request(http.MethodGet, strings.Replace(target, share.Token, other.Token, 1), "", 0); rec.Code != 403 {
		t.Fatalf("other share: %d", rec.Code)
	}
	f.h.Deps.DB.Model(&share).Update("return_upload_link", false)
	if rec := f.request(http.MethodGet, target, "", 0); rec.Code != 403 {
		t.Fatalf("disabled: %d", rec.Code)
	}
	f.h.Deps.DB.Model(&share).Update("return_upload_link", true)
	f.h.Deps.DB.Model(&f.link).Update("parent_folder_id", f.outside.ID)
	if rec := f.request(http.MethodGet, target, "", 0); rec.Code != 403 {
		t.Fatalf("moved video exposed: %d", rec.Code)
	}
}

func TestGuestPublicLinkFailuresDoNotExposeIngestionErrors(t *testing.T) {
	f := newFolderShareFixture(t)
	share := f.create(t, false, true, "")
	create := f.createGuestUpload(share, f.root.ID, uuid.NewString(), 5)
	session := f.completeGuestUpload(t, share, create.Header().Get("Location"), "guest")
	f.h.Deps.DB.Model(&session).Updates(map[string]any{"status": models.UploadStatusFailed, "error": "private storage path and credentials"})
	rec := f.request(http.MethodGet, "/api/shares/"+share.Token+"/uploads/"+session.TusID, "", 0)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"Status":"failed"`) || strings.Contains(rec.Body.String(), "private") {
		t.Fatalf("failure response: %d %s", rec.Code, rec.Body.String())
	}
}
