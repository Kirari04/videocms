package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type walkFailStore struct {
	storage.Store
}

func (walkFailStore) Walk(context.Context, storage.Key, func(storage.ObjectInfo) error) error {
	return errors.New("injected walk failure")
}

func TestEncoderCleanupDeletesSourceFromNamedStoreAndKeepsOutputs(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"),
		&gorm.Config{
			DisableForeignKeyConstraintWhenMigrating: true,
			IgnoreRelationshipsWhenMigrating:         true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.File{}, &models.Quality{}, &models.Audio{}, &models.Subtitle{}); err != nil {
		t.Fatal(err)
	}
	defaultStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	archiveStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storageService, err := storage.NewService("local", storage.LegacyMediaLayout{}, map[string]storage.Store{
		"local":   defaultStore,
		"archive": archiveStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageService.Close() })

	fileUUID := "550e8400-e29b-41d4-a716-446655440301"
	sourceKey, err := storageService.Layout().Source(fileUUID, "original.mp4")
	if err != nil {
		t.Fatal(err)
	}
	outputKey, err := storageService.Layout().Video(fileUUID, "720p", "out0.ts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := archiveStore.Put(context.Background(), sourceKey, strings.NewReader("source"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := archiveStore.Put(context.Background(), outputKey, strings.NewReader("segment"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := defaultStore.Put(context.Background(), sourceKey, strings.NewReader("keep"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	file := models.File{
		UUID:      fileUUID,
		StorageID: "archive",
		SourceKey: sourceKey.String(),
		Size:      100,
	}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}

	worker := &WorkerGroup{deps: &app.Deps{DB: db, Storage: storageService}}
	worker.runEncoderCleanup()

	var updated models.File
	if err := db.First(&updated, file.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.SourceKey != "" || updated.Path != "" {
		t.Fatalf("source references were not cleared: %#v", updated)
	}
	if updated.Size != int64(len("segment")) {
		t.Fatalf("stored size = %d, want %d", updated.Size, len("segment"))
	}
	if _, err := archiveStore.Stat(context.Background(), sourceKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("archive source still exists: %v", err)
	}
	if _, err := archiveStore.Stat(context.Background(), outputKey); err != nil {
		t.Fatalf("encoded output was removed: %v", err)
	}
	if _, err := defaultStore.Stat(context.Background(), sourceKey); err != nil {
		t.Fatalf("default-store source was removed: %v", err)
	}
}

func TestEncoderCleanupKeepsRecordedSizeWhenStoredSizeCannotBeCalculated(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"),
		&gorm.Config{
			DisableForeignKeyConstraintWhenMigrating: true,
			IgnoreRelationshipsWhenMigrating:         true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.File{}, &models.Quality{}, &models.Audio{}, &models.Subtitle{}); err != nil {
		t.Fatal(err)
	}
	archiveStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storageService, err := storage.NewService("archive", storage.LegacyMediaLayout{}, map[string]storage.Store{
		"archive": walkFailStore{Store: archiveStore},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageService.Close() })

	fileUUID := "550e8400-e29b-41d4-a716-446655440302"
	sourceKey, err := storageService.Layout().Source(fileUUID, "original.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := archiveStore.Put(context.Background(), sourceKey, strings.NewReader("source"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	file := models.File{
		UUID:      fileUUID,
		StorageID: "archive",
		SourceKey: sourceKey.String(),
		Size:      100,
	}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}

	worker := &WorkerGroup{deps: &app.Deps{DB: db, Storage: storageService}}
	worker.runEncoderCleanup()

	var updated models.File
	if err := db.First(&updated, file.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.SourceKey != "" {
		t.Fatalf("source reference was not cleared: %#v", updated)
	}
	if updated.Size != file.Size {
		t.Fatalf("stored size = %d, want previous value %d", updated.Size, file.Size)
	}
	if _, err := archiveStore.Stat(context.Background(), sourceKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("archive source still exists: %v", err)
	}
}
