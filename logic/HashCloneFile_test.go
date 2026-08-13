package logic

import (
	"net/http"
	"testing"

	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"
)

func TestCloneFileByHashOnlyReusesAvailableRuntimeStorage(t *testing.T) {
	db := newStorageAdminTestDB(t)
	user := models.User{Username: "clone-user"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	const hash = "same-content"
	for _, file := range []models.File{
		{UUID: "detached", Hash: hash, StorageID: "detached", StorageState: models.FileStorageUnavailable, Size: 10},
		{UUID: "missing-runtime", Hash: hash, StorageID: "missing-runtime", StorageState: models.FileStorageAvailable, Size: 10},
	} {
		if err := db.Create(&file).Error; err != nil {
			t.Fatal(err)
		}
	}
	localStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storageService, err := storage.NewService(models.StorageMountLocalUUID, storage.LegacyMediaLayout{}, map[string]storage.Store{
		models.StorageMountLocalUUID: localStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageService.Close() })
	service := NewService(&app.Deps{DB: db, Storage: storageService})

	status, _, err := service.CloneFileByHash(hash, 0, "duplicate.mp4", user.ID, "")
	if err == nil || status != http.StatusNotFound {
		t.Fatalf("unavailable clone result = status %d, error %v", status, err)
	}
	available := models.File{
		UUID: "available", Hash: hash, StorageID: models.StorageMountLocalUUID, StorageState: models.FileStorageAvailable, Size: 10,
	}
	if err := db.Create(&available).Error; err != nil {
		t.Fatal(err)
	}
	status, link, err := service.CloneFileByHash(hash, 0, "duplicate.mp4", user.ID, "")
	if err != nil || status != http.StatusOK {
		t.Fatalf("available clone result = status %d, error %v", status, err)
	}
	if link == nil || link.FileID != available.ID {
		t.Fatalf("clone link = %#v, want file %d", link, available.ID)
	}
}
