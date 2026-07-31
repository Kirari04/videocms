package cmd

import (
	"context"
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNewStorageServiceAlwaysUsesLocalDefault(t *testing.T) {
	db := newStorageTestDB(t)
	root := t.TempDir()
	service, cipher, err := newStorageService(context.Background(), config.Config{
		FolderVideoQualitysPriv: filepath.Join(root, "qualitys"),
		StorageScratchDir:       filepath.Join(root, "scratch"),
	}, db)
	if err != nil {
		t.Fatalf("newStorageService() error = %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	if cipher != nil {
		t.Fatal("credential cipher is configured without an encryption key")
	}
	if service.DefaultStoreID() != models.StorageMountLocalUUID {
		t.Fatalf("default store = %q, want local", service.DefaultStoreID())
	}
	if _, err := service.Store(models.StorageMountLocalUUID); err != nil {
		t.Fatalf("local store is not registered: %v", err)
	}
}

func TestNewStorageServiceLoadsMountedS3Configuration(t *testing.T) {
	db := newStorageTestDB(t)
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	cipher, err := storage.NewCredentialCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	mountID := "c290f1ee-6c54-4b01-90e6-d701748f0851"
	configuration, credentials, err := storage.EncodeS3Mount(
		storage.S3MountConfiguration{
			Bucket:            "media",
			Region:            "eu-central-1",
			Endpoint:          "http://127.0.0.1:9000",
			UsePathStyle:      true,
			UploadPartSize:    5 * 1024 * 1024,
			UploadConcurrency: 2,
		},
		storage.S3MountCredentials{AccessKeyID: "access", SecretAccessKey: "secret"},
		mountID,
		cipher,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.StorageMount{
		UUID:                 mountID,
		Name:                 "Media",
		Provider:             models.StorageProviderS3,
		Configuration:        configuration,
		EncryptedCredentials: credentials,
		Mounted:              true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	service, loadedCipher, err := newStorageService(context.Background(), config.Config{
		FolderVideoQualitysPriv: filepath.Join(root, "qualitys"),
		StorageScratchDir:       filepath.Join(root, "scratch"),
		StorageEncryptionKey:    key,
	}, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	if loadedCipher == nil {
		t.Fatal("credential cipher was not loaded")
	}
	if _, err := service.Store(mountID); err != nil {
		t.Fatalf("S3 store is not registered: %v", err)
	}
	if _, err := service.Store(models.StorageMountLocalUUID); err != nil {
		t.Fatalf("local store is not registered: %v", err)
	}
}

func TestNewStorageServiceLeavesEncryptedMountUnavailableWithoutKey(t *testing.T) {
	db := newStorageTestDB(t)
	mountID := "f452d8c0-6d0b-4d55-81bb-91501dcca39f"
	if err := db.Create(&models.StorageMount{
		UUID:                 mountID,
		Name:                 "Unavailable",
		Provider:             models.StorageProviderS3,
		Configuration:        `{}`,
		EncryptedCredentials: "v1:encrypted",
		Mounted:              true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	file := models.File{UUID: "file", StorageID: mountID, StorageState: models.FileStorageAvailable}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	service, _, err := newStorageService(context.Background(), config.Config{
		FolderVideoQualitysPriv: filepath.Join(root, "qualitys"),
		StorageScratchDir:       filepath.Join(root, "scratch"),
	}, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	if _, err := service.Store(mountID); !errors.Is(err, storage.ErrStoreNotConfigured) {
		t.Fatalf("unconfigured mount error = %v", err)
	}
	if err := db.First(&file, file.ID).Error; err != nil {
		t.Fatal(err)
	}
	if file.StorageState != models.FileStorageUnavailable {
		t.Fatalf("file storage state = %q", file.StorageState)
	}
	var mount models.StorageMount
	if err := db.Where("uuid = ?", mountID).First(&mount).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mount.LastError, "StorageEncryptionKey") {
		t.Fatalf("mount error = %q", mount.LastError)
	}
}

func TestReconnectMountedStorageFilesOnlyRestoresOwnedMatches(t *testing.T) {
	db := newStorageTestDB(t)
	localStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remoteStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storageService, err := storage.NewService(models.StorageMountLocalUUID, storage.LegacyMediaLayout{}, map[string]storage.Store{
		models.StorageMountLocalUUID: localStore,
		"remote":                     remoteStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageService.Close() })
	mount := models.StorageMount{UUID: "remote", Name: "Remote", Provider: models.StorageProviderS3, Mounted: true}
	if err := db.Create(&mount).Error; err != nil {
		t.Fatal(err)
	}
	matched := models.File{UUID: "matched", StorageID: mount.UUID, StorageState: models.FileStorageUnavailable}
	missing := models.File{UUID: "missing", StorageID: mount.UUID, StorageState: models.FileStorageUnavailable}
	other := models.File{UUID: "matched", StorageID: "other", StorageState: models.FileStorageUnavailable}
	if err := db.Create(&matched).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&missing).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	key, err := storageService.Layout().Video(matched.UUID, "720p", "out0.ts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remoteStore.Put(context.Background(), key, strings.NewReader("segment"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := reconnectMountedStorageFiles(context.Background(), db, storageService, mount); err != nil {
		t.Fatal(err)
	}
	for _, expectation := range []struct {
		file      *models.File
		available bool
	}{
		{file: &matched, available: true},
		{file: &missing, available: false},
		{file: &other, available: false},
	} {
		if err := db.First(expectation.file, expectation.file.ID).Error; err != nil {
			t.Fatal(err)
		}
		gotAvailable := expectation.file.StorageState == models.FileStorageAvailable
		if gotAvailable != expectation.available {
			t.Fatalf("file %d availability = %v, want %v", expectation.file.ID, gotAvailable, expectation.available)
		}
	}
}

func newStorageTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(
		sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"),
		&gorm.Config{DisableForeignKeyConstraintWhenMigrating: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.StorageMount{}, &models.File{}); err != nil {
		t.Fatal(err)
	}
	return db
}
