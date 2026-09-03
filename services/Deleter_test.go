package services

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDeleteStoredFileUsesMediaStoreAndRemovesSource(t *testing.T) {
	defaultStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalStore() error = %v", err)
	}
	archiveStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalStore() archive error = %v", err)
	}
	storageService, err := storage.NewService(
		"local",
		storage.LegacyMediaLayout{},
		map[string]storage.Store{
			"local":   defaultStore,
			"archive": archiveStore,
		},
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	t.Cleanup(func() { _ = storageService.Close() })

	fileUUID := "550e8400-e29b-41d4-a716-446655440300"
	mediaKey, err := storageService.Layout().Video(fileUUID, "720p", "out0.ts")
	if err != nil {
		t.Fatalf("Video() error = %v", err)
	}
	if _, err := archiveStore.Put(context.Background(), mediaKey, strings.NewReader("segment"), storage.PutOptions{}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if _, err := defaultStore.Put(context.Background(), mediaKey, strings.NewReader("keep"), storage.PutOptions{}); err != nil {
		t.Fatalf("Put() default error = %v", err)
	}

	sourcePath := filepath.Join(t.TempDir(), "source.tmp")
	if err := os.WriteFile(sourcePath, []byte("source"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	worker := &WorkerGroup{deps: &app.Deps{Storage: storageService}}
	if err := worker.deleteStoredFile(models.File{UUID: fileUUID, StorageID: "archive", Path: sourcePath}); err != nil {
		t.Fatalf("deleteStoredFile() error = %v", err)
	}
	if _, err := os.Stat(sourcePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source still exists or stat failed unexpectedly: %v", err)
	}
	if _, err := archiveStore.Stat(context.Background(), mediaKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("media object still exists or stat failed unexpectedly: %v", err)
	}
	if _, err := defaultStore.Stat(context.Background(), mediaKey); err != nil {
		t.Fatalf("default-store object was removed: %v", err)
	}
}

func TestDeleterRemovesAllMigrationCopiesAndReleasesReservations(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.File{}, &models.Link{}, &models.Quality{}, &models.Audio{}, &models.Subtitle{}, &models.StorageMigration{}, &models.StorageMigrationAccount{}, &models.StorageMigrationItem{}, &models.StorageMigrationObject{}); err != nil {
		t.Fatal(err)
	}
	source, _ := storage.NewLocalStore(t.TempDir())
	destination, _ := storage.NewLocalStore(t.TempDir())
	storageService, err := storage.NewService("source", storage.LegacyMediaLayout{}, map[string]storage.Store{"source": source, "destination": destination})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageService.Close() })
	migration := models.StorageMigration{UUID: "delete-migrating-files", Status: models.StorageMigrationRunning, FileCount: 2, PlannedBytes: 11}
	if err := db.Create(&migration).Error; err != nil {
		t.Fatal(err)
	}

	preCutover := models.File{UUID: "550e8400-e29b-41d4-a716-446655440310", StorageID: "source", StorageState: models.FileStorageAvailable}
	postCutover := models.File{UUID: "550e8400-e29b-41d4-a716-446655440311", StorageID: "destination", StorageState: models.FileStorageAvailable}
	for _, file := range []*models.File{&preCutover, &postCutover} {
		if err := db.Create(file).Error; err != nil {
			t.Fatal(err)
		}
		link := models.Link{UUID: file.UUID + "-link", Name: "deleted", FileID: file.ID}
		if err := db.Create(&link).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Delete(&link).Error; err != nil {
			t.Fatal(err)
		}
	}
	preKey := migrationTestKey(t, preCutover.UUID+"/source/original.mp4")
	postKey := migrationTestKey(t, postCutover.UUID+"/source/original.mp4")
	putMigrationTestObject(t, source, preKey, "source")
	putMigrationTestObject(t, destination, preKey, "partial")
	putMigrationTestObject(t, source, postKey, "retained")
	putMigrationTestObject(t, destination, postKey, "active")
	now := time.Now().UTC()
	items := []models.StorageMigrationItem{
		{MigrationID: migration.ID, FileID: preCutover.ID, FileUUID: preCutover.UUID, SourceMountID: "source", DestinationMountID: "destination", Status: models.StorageMigrationItemCopying, ReservationKey: fmt.Sprintf("file:%d", preCutover.ID), DestinationOwned: true, PlannedBytes: 5, BytesTotal: 5, BytesCopied: 5},
		{MigrationID: migration.ID, FileID: postCutover.ID, FileUUID: postCutover.UUID, SourceMountID: "source", DestinationMountID: "destination", Status: models.StorageMigrationItemCleanupPending, ReservationKey: fmt.Sprintf("file:%d", postCutover.ID), DestinationOwned: true, PlannedBytes: 6, BytesTotal: 6, BytesCopied: 6, CutoverAt: &now},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if err := db.Create(&models.StorageMigrationObject{ItemID: item.ID, ObjectKey: fmt.Sprintf("checkpoint/%d", item.ID), SourceRevision: "delete"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	worker := NewWorkerGroup(&app.Deps{DB: db, Storage: storageService}, nil)
	if err := worker.runDeleter(); err != nil {
		t.Fatal(err)
	}
	for _, file := range []models.File{preCutover, postCutover} {
		var current models.File
		if err := db.Unscoped().First(&current, file.ID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("file %d still exists: %v", file.ID, err)
		}
	}
	for _, pair := range []struct {
		store storage.Store
		key   storage.Key
	}{{source, preKey}, {destination, preKey}, {source, postKey}, {destination, postKey}} {
		if _, err := pair.store.Stat(context.Background(), pair.key); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("migration copy %s remained: %v", pair.key.String(), err)
		}
	}
	for index := range items {
		if err := db.First(&items[index], items[index].ID).Error; err != nil {
			t.Fatal(err)
		}
		if items[index].Status != models.StorageMigrationItemDeleted || items[index].ReservationKey != "" || items[index].DestinationOwned {
			t.Fatalf("migration item was not finalized: %#v", items[index])
		}
	}
	if err := db.First(&migration, migration.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migration.DeletedCount != 2 || migration.ActualBytes != 0 || migration.CopiedBytes != 0 {
		t.Fatalf("migration progress was not reconciled: %#v", migration)
	}
	var checkpointCount int64
	if err := db.Model(&models.StorageMigrationObject{}).Count(&checkpointCount).Error; err != nil {
		t.Fatal(err)
	}
	if checkpointCount != 0 {
		t.Fatalf("%d deleted-file checkpoint(s) remained", checkpointCount)
	}
}

func TestDeleterKeepsFileWithActiveLink(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.File{}, &models.Link{}, &models.Quality{}, &models.Audio{}, &models.Subtitle{}); err != nil {
		t.Fatal(err)
	}
	store, _ := storage.NewLocalStore(t.TempDir())
	storageService, _ := storage.NewService("source", storage.LegacyMediaLayout{}, map[string]storage.Store{"source": store})
	t.Cleanup(func() { _ = storageService.Close() })
	file := models.File{UUID: "550e8400-e29b-41d4-a716-446655440312", StorageID: "source", StorageState: models.FileStorageAvailable}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Link{UUID: "active-link", Name: "active", FileID: file.ID}).Error; err != nil {
		t.Fatal(err)
	}
	worker := NewWorkerGroup(&app.Deps{DB: db, Storage: storageService}, nil)
	if err := worker.runDeleter(); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&models.File{}, file.ID).Error; err != nil {
		t.Fatalf("referenced file was deleted: %v", err)
	}
}
