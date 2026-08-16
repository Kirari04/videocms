package logic

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/background"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestStorageMigrationPreviewAndStartSnapshotDeterministically(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.StorageMount{}, &models.StoragePool{}, &models.StoragePoolMount{}, &models.User{}, &models.File{}, &models.Link{}, &models.StorageMigration{}, &models.StorageMigrationItem{}); err != nil {
		t.Fatal(err)
	}
	if err := background.Migrate(db); err != nil {
		t.Fatal(err)
	}
	stores := make(map[string]storage.Store)
	mounts := make([]models.StorageMount, 0, 3)
	for _, id := range []string{"source", "destination-a", "destination-b"} {
		store, err := storage.NewLocalStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		stores[id] = store
		mount := models.StorageMount{UUID: id, Name: id, Provider: models.StorageProviderLocal, Mounted: true}
		if err := db.Create(&mount).Error; err != nil {
			t.Fatal(err)
		}
		mounts = append(mounts, mount)
	}
	storageService, err := storage.NewService("source", storage.LegacyMediaLayout{}, stores)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageService.Close() })
	sourcePool := models.StoragePool{UUID: "source-pool", Name: "Source", IsDefault: true}
	destinationPool := models.StoragePool{UUID: "destination-pool", Name: "Destination"}
	if err := db.Create(&sourcePool).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&destinationPool).Error; err != nil {
		t.Fatal(err)
	}
	for _, membership := range []models.StoragePoolMount{
		{StoragePoolID: sourcePool.ID, StorageMountID: mounts[0].ID},
		{StoragePoolID: destinationPool.ID, StorageMountID: mounts[1].ID},
		{StoragePoolID: destinationPool.ID, StorageMountID: mounts[2].ID},
	} {
		if err := db.Create(&membership).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []models.File{
		{UUID: "550e8400-e29b-41d4-a716-446655440301", StorageID: "source", StorageState: models.FileStorageAvailable, Size: 100},
		{UUID: "550e8400-e29b-41d4-a716-446655440302", StorageID: "source", StorageState: models.FileStorageAvailable, Size: 50},
	} {
		if err := db.Create(&file).Error; err != nil {
			t.Fatal(err)
		}
	}
	runtime := background.New(db, background.Options{})
	service := NewService(&app.Deps{DB: db, Storage: storageService, Background: runtime})
	input := StorageMigrationInput{SourcePoolID: sourcePool.ID, DestinationPoolID: destinationPool.ID}
	preview, err := service.PreviewStorageMigration(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if preview.FileCount != 2 || preview.PlannedBytes != 150 || preview.CleanupGraceHours != 24 || len(preview.DestinationPlacements) != 2 {
		t.Fatalf("unexpected preview: %#v", preview)
	}
	input.PlanFingerprint = preview.PlanFingerprint
	input.IdempotencyKey = "migration-request"
	if err := db.Model(&models.File{}).Where("uuid = ?", "550e8400-e29b-41d4-a716-446655440301").Update("size", 101).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.StartStorageMigration(context.Background(), input, 7, "admin"); !errors.Is(err, ErrStorageMigrationConflict) {
		t.Fatalf("expected changed preview to be rejected, got %v", err)
	}
	if err := db.Model(&models.File{}).Where("uuid = ?", "550e8400-e29b-41d4-a716-446655440301").Update("size", 100).Error; err != nil {
		t.Fatal(err)
	}
	migration, job, err := service.StartStorageMigration(context.Background(), input, 7, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if migration.FileCount != 2 || migration.BackgroundJobID == "" || job.Kind != "storage.migration" {
		t.Fatalf("unexpected migration: %#v job=%#v", migration, job)
	}
	summary, err := service.GetStorageMigrationSummary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Active != 1 || summary.RetainingOriginals != 0 || summary.NeedsAttention != 0 || summary.VideosMoved != 0 {
		t.Fatalf("unexpected migration summary: %#v", summary)
	}
	var items []models.StorageMigrationItem
	if err := db.Where("migration_id = ?", migration.ID).Order("file_id ASC").Find(&items).Error; err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].DestinationMountID == items[1].DestinationMountID || items[0].ReservationKey == "" || items[1].ReservationKey == "" {
		t.Fatalf("unexpected deterministic assignments: %#v", items)
	}
	reusedMigration, reusedJob, err := service.StartStorageMigration(context.Background(), input, 7, "admin")
	if err != nil || reusedMigration.ID != migration.ID || reusedJob.ID != job.ID {
		t.Fatalf("idempotent migration request was not reused: migration=%#v job=%#v err=%v", reusedMigration, reusedJob, err)
	}
	input.PlanFingerprint = strings.Repeat("0", 64)
	if _, _, err := service.StartStorageMigration(context.Background(), input, 7, "admin"); !errors.Is(err, ErrStorageMigrationConflict) {
		t.Fatalf("expected changed idempotent request to conflict, got %v", err)
	}
	input.PlanFingerprint = preview.PlanFingerprint
	active, err := service.ListStorageMigrations(context.Background(), "active", 50, 0)
	if err != nil || len(active) != 1 || active[0].ID != migration.ID {
		t.Fatalf("active server filter did not return migration: %#v err=%v", active, err)
	}
	complete, err := service.ListStorageMigrations(context.Background(), "complete", 50, 0)
	if err != nil || len(complete) != 0 {
		t.Fatalf("complete server filter returned active migration: %#v err=%v", complete, err)
	}
	if _, err := service.ListStorageMigrations(context.Background(), "unknown", 50, 0); !errors.Is(err, ErrStorageMigrationConflict) {
		t.Fatalf("expected invalid status filter to fail, got %v", err)
	}
	input.IdempotencyKey = "another-migration-request"
	if _, _, err := service.StartStorageMigration(context.Background(), input, 7, "admin"); !errors.Is(err, ErrStorageMigrationConflict) {
		t.Fatalf("expected active reservation conflict, got %v", err)
	}
}

func TestStorageMigrationRejectsOverlappingPools(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.StorageMount{}, &models.StoragePool{}, &models.StoragePoolMount{}, &models.File{}, &models.StorageMigrationItem{}); err != nil {
		t.Fatal(err)
	}
	store, _ := storage.NewLocalStore(t.TempDir())
	storageService, _ := storage.NewService("shared", storage.LegacyMediaLayout{}, map[string]storage.Store{"shared": store})
	t.Cleanup(func() { _ = storageService.Close() })
	mount := models.StorageMount{UUID: "shared", Name: "Shared", Provider: models.StorageProviderLocal, Mounted: true}
	db.Create(&mount)
	first := models.StoragePool{UUID: "first", Name: "First"}
	second := models.StoragePool{UUID: "second", Name: "Second"}
	db.Create(&first)
	db.Create(&second)
	db.Create(&models.StoragePoolMount{StoragePoolID: first.ID, StorageMountID: mount.ID})
	db.Create(&models.StoragePoolMount{StoragePoolID: second.ID, StorageMountID: mount.ID})
	service := NewService(&app.Deps{DB: db, Storage: storageService})
	_, err = service.PreviewStorageMigration(context.Background(), StorageMigrationInput{SourcePoolID: first.ID, DestinationPoolID: second.ID})
	if !errors.Is(err, ErrStorageMigrationConflict) {
		t.Fatalf("expected overlap conflict, got %v", err)
	}
}

func TestKeepStorageMigrationOriginalsWaitsForInFlightCleanup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.StorageMigration{}, &models.StorageMigrationItem{}); err != nil {
		t.Fatal(err)
	}
	migration := models.StorageMigration{UUID: "keep-originals", Status: models.StorageMigrationCleaningOriginals, FileCount: 2}
	if err := db.Create(&migration).Error; err != nil {
		t.Fatal(err)
	}
	item := models.StorageMigrationItem{MigrationID: migration.ID, FileID: 42, FileUUID: "file", Status: models.StorageMigrationItemCleaning, ReservationKey: "file:42"}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	partial := models.StorageMigrationItem{MigrationID: migration.ID, FileID: 43, FileUUID: "partial", Status: models.StorageMigrationItemCleaning, ReservationKey: "file:43"}
	if err := db.Create(&partial).Error; err != nil {
		t.Fatal(err)
	}
	deps := &app.Deps{DB: db}
	service := NewService(deps)
	releaseCleanup := deps.StorageLifecycle.FileWriteLock(item.FileID)
	result := make(chan models.StorageMigration, 1)
	failures := make(chan error, 1)
	go func() {
		kept, keepErr := service.KeepStorageMigrationOriginals(context.Background(), migration.UUID, 1, "admin")
		if keepErr != nil {
			failures <- keepErr
			return
		}
		result <- kept
	}()
	select {
	case <-result:
		t.Fatal("keep originals did not wait for active cleanup")
	case err := <-failures:
		t.Fatalf("keep originals failed early: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	now := time.Now().UTC()
	if err := db.Model(&item).Updates(map[string]any{"status": models.StorageMigrationItemCleaned, "cleaned_at": &now, "reservation_key": ""}).Error; err != nil {
		t.Fatal(err)
	}
	releaseCleanup()
	select {
	case kept := <-result:
		if kept.Status != models.StorageMigrationOriginalsRetained || kept.CleanedCount != 1 {
			t.Fatalf("unexpected retained state: %#v", kept)
		}
	case err := <-failures:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("keep originals did not finish after cleanup checkpoint")
	}
	if err := db.First(&item, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != models.StorageMigrationItemCleaned {
		t.Fatalf("completed cleanup was overwritten: %#v", item)
	}
	if err := db.First(&partial, partial.ID).Error; err != nil {
		t.Fatal(err)
	}
	if partial.Status != models.StorageMigrationItemOriginalPartial || partial.ReservationKey != "" {
		t.Fatalf("incomplete cleanup was mislabeled as a complete original: %#v", partial)
	}
}

func TestCanceledStorageMigrationProtectsMountsDuringSafetyWindow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.StorageMigration{}, &models.StorageMigrationItem{}); err != nil {
		t.Fatal(err)
	}
	protectUntil := time.Now().UTC().Add(time.Hour)
	migration := models.StorageMigration{UUID: "canceled-grace", Status: models.StorageMigrationCanceled, CleanupAfter: &protectUntil}
	if err := db.Create(&migration).Error; err != nil {
		t.Fatal(err)
	}
	item := models.StorageMigrationItem{MigrationID: migration.ID, FileID: 1, SourceMountID: "source", DestinationMountID: "destination", Status: models.StorageMigrationItemCanceled}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	service := NewService(&app.Deps{DB: db})
	if err := service.ensureStorageMountNotMigrating("source"); !errors.Is(err, ErrStorageMigrationConflict) {
		t.Fatalf("source mount was not protected: %v", err)
	}
	if err := service.ensureStorageMountNotMigrating("destination"); !errors.Is(err, ErrStorageMigrationConflict) {
		t.Fatalf("destination mount was not protected: %v", err)
	}
	expired := time.Now().UTC().Add(-time.Hour)
	if err := db.Model(&migration).Update("cleanup_after", &expired).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.ensureStorageMountNotMigrating("source"); err != nil {
		t.Fatalf("expired safety window still blocked mount: %v", err)
	}
}

func TestFailedStorageMigrationCanBeCanceledForReconciliation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.StorageMigration{}, &models.StorageMigrationItem{}); err != nil {
		t.Fatal(err)
	}
	if err := background.Migrate(db); err != nil {
		t.Fatal(err)
	}
	migration := models.StorageMigration{UUID: "failed-cancel", SourcePoolName: "Source", DestinationPoolName: "Destination", Status: models.StorageMigrationFailed, FileCount: 1}
	if err := db.Create(&migration).Error; err != nil {
		t.Fatal(err)
	}
	item := models.StorageMigrationItem{MigrationID: migration.ID, FileID: 1, FileUUID: "file", SourceMountID: "source", DestinationMountID: "destination", Status: models.StorageMigrationItemFailed, ReservationKey: "file:1", DestinationOwned: true}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	runtime := background.New(db, background.Options{})
	service := NewService(&app.Deps{DB: db, Background: runtime})
	canceled, err := service.CancelFailedStorageMigration(context.Background(), migration.UUID)
	if err != nil {
		t.Fatal(err)
	}
	if canceled.Status != models.StorageMigrationCanceled || canceled.CleanupJobID == "" || canceled.CleanupAfter == nil || canceled.CanceledAt == nil {
		t.Fatalf("failed migration was not queued for safe reconciliation: %#v", canceled)
	}
	job, err := runtime.Job(context.Background(), canceled.CleanupJobID, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if job.Kind != "storage.migration.abort_cleanup" || job.Status != background.JobQueued {
		t.Fatalf("unexpected reconciliation job: %#v", job)
	}
	firstCleanupJobID := canceled.CleanupJobID
	if err := db.Model(&canceled).Updates(map[string]any{"status": models.StorageMigrationFailed, "cleanup_job_id": ""}).Error; err != nil {
		t.Fatal(err)
	}
	canceledAgain, err := service.CancelFailedStorageMigration(context.Background(), migration.UUID)
	if err != nil {
		t.Fatal(err)
	}
	if canceledAgain.CancelGeneration != 2 || canceledAgain.CleanupJobID == firstCleanupJobID {
		t.Fatalf("later cancellation reused stale cleanup job: first=%s migration=%#v", firstCleanupJobID, canceledAgain)
	}
}
