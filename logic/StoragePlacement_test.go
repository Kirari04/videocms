package logic

import (
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
	for _, mount := range []*models.StorageMount{&mountA, &mountB, &mountC} {
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
	for _, id := range []string{mountA.UUID, mountB.UUID, mountC.UUID} {
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
