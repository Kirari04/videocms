package services

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
