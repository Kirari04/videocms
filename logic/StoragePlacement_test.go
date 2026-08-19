package logic

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUploadStoreCandidatesUsesLeastTrackedBytesAndUserOverride(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.StorageMount{},
		&models.StoragePool{},
		&models.StoragePoolMount{},
		&models.User{},
		&models.File{},
	); err != nil {
		t.Fatal(err)
	}
	mountA := models.StorageMount{UUID: "mount-a", Name: "A", Provider: models.StorageProviderS3, Mounted: true}
	mountB := models.StorageMount{UUID: "mount-b", Name: "B", Provider: models.StorageProviderS3, Mounted: true}
	mountC := models.StorageMount{UUID: "mount-c", Name: "C", Provider: models.StorageProviderS3, Mounted: true}
	mountD := models.StorageMount{UUID: "mount-d", Name: "Unhealthy", Provider: models.StorageProviderS3, Mounted: true, LastError: "connection failed"}
	mountE := models.StorageMount{UUID: "mount-e", Name: "Built-in cache", Provider: models.StorageProviderLocal, Mounted: true, System: true}
	for _, mount := range []*models.StorageMount{&mountA, &mountB, &mountC, &mountD, &mountE} {
		if err := db.Create(mount).Error; err != nil {
			t.Fatal(err)
		}
	}
	defaultPool := models.StoragePool{UUID: "default", Name: "Default", IsDefault: true}
	overridePool := models.StoragePool{UUID: "override", Name: "Override"}
	if err := db.Create(&defaultPool).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&overridePool).Error; err != nil {
		t.Fatal(err)
	}
	for _, member := range []models.StoragePoolMount{
		{StoragePoolID: defaultPool.ID, StorageMountID: mountA.ID},
		{StoragePoolID: defaultPool.ID, StorageMountID: mountB.ID},
		{StoragePoolID: defaultPool.ID, StorageMountID: mountD.ID},
		{StoragePoolID: defaultPool.ID, StorageMountID: mountE.ID, Role: models.StoragePoolMountCache, CacheMaxBytes: 1024},
		{StoragePoolID: overridePool.ID, StorageMountID: mountC.ID},
	} {
		if err := db.Create(&member).Error; err != nil {
			t.Fatal(err)
		}
	}
	user := models.User{Username: "default-user"}
	overrideUser := models.User{Username: "override-user", StoragePoolID: &overridePool.ID}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&overrideUser).Error; err != nil {
		t.Fatal(err)
	}
	for _, file := range []models.File{
		{UUID: "a-used", StorageID: mountA.UUID, StorageState: models.FileStorageAvailable, Size: 100},
		{UUID: "b-used", StorageID: mountB.UUID, StorageState: models.FileStorageAvailable, Size: 10},
		{UUID: "b-unavailable", StorageID: mountB.UUID, StorageState: models.FileStorageUnavailable, Size: 1000},
	} {
		if err := db.Create(&file).Error; err != nil {
			t.Fatal(err)
		}
	}

	stores := map[string]storage.Store{}
	for _, id := range []string{mountA.UUID, mountB.UUID, mountC.UUID, mountD.UUID, mountE.UUID} {
		store, err := storage.NewLocalStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		stores[id] = store
	}
	storageService, err := storage.NewService(mountA.UUID, storage.LegacyMediaLayout{}, stores)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageService.Close() })
	service := &Service{Deps: &app.Deps{DB: db, Storage: storageService}}

	candidates, err := service.UploadStoreCandidates(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0] != mountB.UUID || candidates[1] != mountA.UUID {
		t.Fatalf("default candidates = %v, want [mount-b mount-a]", candidates)
	}
	candidates, err = service.UploadStoreCandidates(overrideUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0] != mountC.UUID {
		t.Fatalf("override candidates = %v, want [mount-c]", candidates)
	}
}

func TestPublishUploadSourceCleansPartialObjectBeforeFallback(t *testing.T) {
	db := newStorageAdminTestDB(t)
	failedMount := models.StorageMount{UUID: "mount-a", Name: "Fails", Provider: models.StorageProviderS3, Mounted: true}
	successMount := models.StorageMount{UUID: "mount-b", Name: "Succeeds", Provider: models.StorageProviderS3, Mounted: true}
	for _, mount := range []*models.StorageMount{&failedMount, &successMount} {
		if err := db.Create(mount).Error; err != nil {
			t.Fatal(err)
		}
	}
	pool := models.StoragePool{UUID: "default", Name: "Default", IsDefault: true}
	if err := db.Create(&pool).Error; err != nil {
		t.Fatal(err)
	}
	for _, member := range []models.StoragePoolMount{
		{StoragePoolID: pool.ID, StorageMountID: failedMount.ID},
		{StoragePoolID: pool.ID, StorageMountID: successMount.ID},
	} {
		if err := db.Create(&member).Error; err != nil {
			t.Fatal(err)
		}
	}
	user := models.User{Username: "uploader"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	failedLocal, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	successLocal, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storageService, err := storage.NewService(failedMount.UUID, storage.LegacyMediaLayout{}, map[string]storage.Store{
		failedMount.UUID:  &partialPutFailureStore{Store: failedLocal},
		successMount.UUID: successLocal,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageService.Close() })
	service := NewService(&app.Deps{DB: db, Storage: storageService})
	key, err := storageService.Layout().Source("new-file", "original.mp4")
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(t.TempDir(), "source.mp4")
	if err := os.WriteFile(sourcePath, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}

	storeID, release, err := service.publishUploadSource(context.Background(), user.ID, key, sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if storeID != successMount.UUID {
		t.Fatalf("selected store = %q, want %q", storeID, successMount.UUID)
	}
	if _, err := failedLocal.Stat(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("partial object remains on failed candidate: %v", err)
	}
	if _, err := successLocal.Stat(context.Background(), key); err != nil {
		t.Fatalf("fallback object not published: %v", err)
	}
}

type partialPutFailureStore struct {
	storage.Store
}

func (s *partialPutFailureStore) Put(ctx context.Context, key storage.Key, src io.Reader, opts storage.PutOptions) (storage.ObjectInfo, error) {
	if _, err := s.Store.Put(ctx, key, src, opts); err != nil {
		return storage.ObjectInfo{}, err
	}
	return storage.ObjectInfo{}, errors.New("simulated provider failure after write")
}
