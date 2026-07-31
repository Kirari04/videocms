package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/storage"
)

func TestNewStorageServiceUsesLocalStoreByDefault(t *testing.T) {
	root := t.TempDir()
	service, err := newStorageService(context.Background(), config.Config{
		FolderVideoQualitysPriv: filepath.Join(root, "qualitys"),
		StorageScratchDir:       filepath.Join(root, "scratch"),
		StorageDefaultStore:     "local",
	})
	if err != nil {
		t.Fatalf("newStorageService() error = %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	if service.DefaultStoreID() != "local" {
		t.Fatalf("default store = %q, want local", service.DefaultStoreID())
	}
	if _, err := service.Store("local"); err != nil {
		t.Fatalf("local store is not registered: %v", err)
	}
	if _, err := service.Store("s3"); !errors.Is(err, storage.ErrStoreNotConfigured) {
		t.Fatalf("S3 store error = %v, want ErrStoreNotConfigured", err)
	}
}

func TestNewStorageServiceRegistersS3WithoutDroppingLocal(t *testing.T) {
	root := t.TempDir()
	usePathStyle := true
	service, err := newStorageService(context.Background(), config.Config{
		FolderVideoQualitysPriv:    filepath.Join(root, "qualitys"),
		StorageScratchDir:          filepath.Join(root, "scratch"),
		StorageDefaultStore:        "s3",
		StorageS3Bucket:            "media",
		StorageS3Region:            "eu-central-1",
		StorageS3Endpoint:          "http://127.0.0.1:9000",
		StorageS3AccessKeyID:       "access",
		StorageS3SecretAccessKey:   "secret",
		StorageS3UsePathStyle:      &usePathStyle,
		StorageS3UploadPartSize:    5 * 1024 * 1024,
		StorageS3UploadConcurrency: 2,
	})
	if err != nil {
		t.Fatalf("newStorageService() error = %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	if service.DefaultStoreID() != "s3" {
		t.Fatalf("default store = %q, want s3", service.DefaultStoreID())
	}
	if _, err := service.Store("s3"); err != nil {
		t.Fatalf("S3 store is not registered: %v", err)
	}
	if _, err := service.Store("local"); err != nil {
		t.Fatalf("local store is not registered for legacy records: %v", err)
	}
}

func TestNewStorageServiceRequiresBucketForS3Default(t *testing.T) {
	root := t.TempDir()
	_, err := newStorageService(context.Background(), config.Config{
		FolderVideoQualitysPriv: filepath.Join(root, "qualitys"),
		StorageScratchDir:       filepath.Join(root, "scratch"),
		StorageDefaultStore:     "s3",
	})
	if err == nil {
		t.Fatal("newStorageService() error = nil")
	}
}
