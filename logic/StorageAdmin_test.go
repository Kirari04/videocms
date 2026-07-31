package logic

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

func TestStorageMountReconnectAndUnmountLifecycle(t *testing.T) {
	db := newStorageAdminTestDB(t)
	localStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remoteStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storageService, err := storage.NewService("local", storage.LegacyMediaLayout{}, map[string]storage.Store{
		"local":  localStore,
		"remote": remoteStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageService.Close() })
	service := &Service{Deps: &app.Deps{DB: db, Storage: storageService}}

	mount := models.StorageMount{UUID: "remote", Name: "Remote", Provider: models.StorageProviderS3, Mounted: true}
	if err := db.Create(&mount).Error; err != nil {
		t.Fatal(err)
	}
	pool := models.StoragePool{UUID: "pool", Name: "Pool", IsDefault: true}
	if err := db.Create(&pool).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.StoragePoolMount{StoragePoolID: pool.ID, StorageMountID: mount.ID}).Error; err != nil {
		t.Fatal(err)
	}
	found := models.File{UUID: "found-file", StorageID: "missing-old-mount", StorageState: models.FileStorageUnavailable, Size: 10}
	missing := models.File{UUID: "missing-file", StorageID: "missing-old-mount", StorageState: models.FileStorageUnavailable, Size: 20}
	if err := db.Create(&found).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&missing).Error; err != nil {
		t.Fatal(err)
	}
	objectKey, err := storageService.Layout().Video(found.UUID, "720p", "out0.ts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remoteStore.Put(context.Background(), objectKey, strings.NewReader("segment"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}

	preview, err := service.ReconnectStorageMount(context.Background(), mount.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Scanned != 2 || preview.Matched != 1 || preview.Relinked != 0 {
		t.Fatalf("preview = %#v", preview)
	}
	if err := db.First(&found, found.ID).Error; err != nil {
		t.Fatal(err)
	}
	if found.StorageState != models.FileStorageUnavailable || found.StorageID != "missing-old-mount" {
		t.Fatalf("preview changed file = %#v", found)
	}

	applied, err := service.ReconnectStorageMount(context.Background(), mount.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Relinked != 1 {
		t.Fatalf("applied = %#v", applied)
	}
	if err := db.First(&found, found.ID).Error; err != nil {
		t.Fatal(err)
	}
	if found.StorageState != models.FileStorageAvailable || found.StorageID != mount.UUID {
		t.Fatalf("relinked file = %#v", found)
	}

	unavailable, err := service.UnmountStorageMount(mount.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unavailable != 1 {
		t.Fatalf("unavailable files = %d, want 1", unavailable)
	}
	if err := db.First(&found, found.ID).Error; err != nil {
		t.Fatal(err)
	}
	if found.StorageState != models.FileStorageUnavailable {
		t.Fatalf("unmounted file state = %q", found.StorageState)
	}
	if _, err := storageService.Store(mount.UUID); !errors.Is(err, storage.ErrStoreNotConfigured) {
		t.Fatalf("unmounted store error = %v", err)
	}
	var membershipCount int64
	if err := db.Model(&models.StoragePoolMount{}).Where("storage_mount_id = ?", mount.ID).Count(&membershipCount).Error; err != nil {
		t.Fatal(err)
	}
	if membershipCount != 1 {
		t.Fatalf("pool memberships = %d, want retained membership", membershipCount)
	}
}

func TestDeleteStoragePoolFallsBackToLocalAndClearsUserOverrides(t *testing.T) {
	db := newStorageAdminTestDB(t)
	localMount := models.StorageMount{UUID: models.StorageMountLocalUUID, Name: "Local", Provider: models.StorageProviderLocal, Mounted: true, System: true}
	remoteMount := models.StorageMount{UUID: "remote", Name: "Remote", Provider: models.StorageProviderS3, Mounted: true}
	if err := db.Create(&localMount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&remoteMount).Error; err != nil {
		t.Fatal(err)
	}
	localPool := models.StoragePool{UUID: models.StoragePoolLocalUUID, Name: "Local", System: true}
	if err := db.Create(&localPool).Error; err != nil {
		t.Fatal(err)
	}
	service := &Service{Deps: &app.Deps{DB: db}}
	remotePool, err := service.CreateStoragePool(StoragePoolInput{Name: "Remote", MountIDs: []uint{remoteMount.ID}, IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	user := models.User{Username: "user", StoragePoolID: &remotePool.ID}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteStoragePool(remotePool.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&localPool, localPool.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !localPool.IsDefault {
		t.Fatal("local pool was not restored as default")
	}
	if err := db.First(&user, user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if user.StoragePoolID != nil {
		t.Fatalf("user storage pool override = %v, want inherit", user.StoragePoolID)
	}
}

func TestUpdatingDefaultStoragePoolWithoutDefaultFallsBackToLocal(t *testing.T) {
	db := newStorageAdminTestDB(t)
	localMount := models.StorageMount{UUID: models.StorageMountLocalUUID, Name: "Local", Provider: models.StorageProviderLocal, Mounted: true, System: true}
	remoteMount := models.StorageMount{UUID: "remote", Name: "Remote", Provider: models.StorageProviderS3, Mounted: true}
	if err := db.Create(&localMount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&remoteMount).Error; err != nil {
		t.Fatal(err)
	}
	localPool := models.StoragePool{UUID: models.StoragePoolLocalUUID, Name: "Local", System: true}
	if err := db.Create(&localPool).Error; err != nil {
		t.Fatal(err)
	}
	service := &Service{Deps: &app.Deps{DB: db}}
	remotePool, err := service.CreateStoragePool(StoragePoolInput{Name: "Remote", MountIDs: []uint{remoteMount.ID}, IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateStoragePool(remotePool.ID, StoragePoolInput{Name: "Remote", MountIDs: []uint{remoteMount.ID}, IsDefault: false}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&localPool, localPool.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !localPool.IsDefault {
		t.Fatal("local pool was not restored as default")
	}
	var defaultCount int64
	if err := db.Model(&models.StoragePool{}).Where("is_default = ?", true).Count(&defaultCount).Error; err != nil {
		t.Fatal(err)
	}
	if defaultCount != 1 {
		t.Fatalf("default pool count = %d, want 1", defaultCount)
	}
}

func newStorageAdminTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{
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
	return db
}
