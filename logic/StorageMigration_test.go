package logic

import (
	"context"
	"errors"
	"fmt"
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
	if err := db.AutoMigrate(&models.StorageMount{}, &models.StoragePool{}, &models.StoragePoolMount{}, &models.User{}, &models.File{}, &models.Link{}, &models.StorageMigration{}, &models.StorageMigrationAccount{}, &models.StorageMigrationItem{}); err != nil {
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
	if preview.Scope != models.StorageMigrationScopeAll || preview.Accounts == nil || preview.FileCount != 2 || preview.PlannedBytes != 150 || preview.CleanupGraceHours != 24 || len(preview.DestinationPlacements) != 2 {
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

func TestStorageMigrationAccountScopeUsesActiveLinksAndPersistsSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.StorageMount{}, &models.StoragePool{}, &models.StoragePoolMount{}, &models.User{}, &models.File{}, &models.Link{}, &models.StorageMigration{}, &models.StorageMigrationAccount{}, &models.StorageMigrationItem{}); err != nil {
		t.Fatal(err)
	}
	if err := background.Migrate(db); err != nil {
		t.Fatal(err)
	}
	stores := make(map[string]storage.Store)
	var mounts []models.StorageMount
	for _, id := range []string{"account-source", "account-destination"} {
		store, storeErr := storage.NewLocalStore(t.TempDir())
		if storeErr != nil {
			t.Fatal(storeErr)
		}
		stores[id] = store
		mount := models.StorageMount{UUID: id, Name: id, Provider: models.StorageProviderLocal, Mounted: true}
		if err := db.Create(&mount).Error; err != nil {
			t.Fatal(err)
		}
		mounts = append(mounts, mount)
	}
	storageService, err := storage.NewService("account-source", storage.LegacyMediaLayout{}, stores)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageService.Close() })
	sourcePool := models.StoragePool{UUID: "account-source-pool", Name: "Account source"}
	destinationPool := models.StoragePool{UUID: "account-destination-pool", Name: "Account destination"}
	if err := db.Create(&sourcePool).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&destinationPool).Error; err != nil {
		t.Fatal(err)
	}
	for _, membership := range []models.StoragePoolMount{
		{StoragePoolID: sourcePool.ID, StorageMountID: mounts[0].ID},
		{StoragePoolID: destinationPool.ID, StorageMountID: mounts[1].ID},
	} {
		if err := db.Create(&membership).Error; err != nil {
			t.Fatal(err)
		}
	}
	users := []models.User{{Username: "alice"}, {Username: "bob"}, {Username: "carol"}, {Username: "deleted"}}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&users[3]).Error; err != nil {
		t.Fatal(err)
	}
	files := []models.File{
		{UUID: "account-alice", StorageID: mounts[0].UUID, StorageState: models.FileStorageAvailable, Size: 100},
		{UUID: "account-bob", StorageID: mounts[0].UUID, StorageState: models.FileStorageAvailable, Size: 80},
		{UUID: "account-alice-carol", StorageID: mounts[0].UUID, StorageState: models.FileStorageAvailable, Size: 60},
		{UUID: "account-alice-bob", StorageID: mounts[0].UUID, StorageState: models.FileStorageAvailable, Size: 40},
		{UUID: "account-carol", StorageID: mounts[0].UUID, StorageState: models.FileStorageAvailable, Size: 20},
		{UUID: "account-carol-unavailable", StorageID: mounts[0].UUID, StorageState: models.FileStorageUnavailable, Size: 10},
	}
	if err := db.Create(&files).Error; err != nil {
		t.Fatal(err)
	}
	links := []models.Link{
		{UUID: "link-alice", Name: "Alice", FileID: files[0].ID, UserID: users[0].ID},
		{UUID: "link-bob", Name: "Bob", FileID: files[1].ID, UserID: users[1].ID},
		{UUID: "link-alice-shared", Name: "Alice shared", FileID: files[2].ID, UserID: users[0].ID},
		{UUID: "link-carol-shared", Name: "Carol shared", FileID: files[2].ID, UserID: users[2].ID},
		{UUID: "link-alice-selected", Name: "Alice selected", FileID: files[3].ID, UserID: users[0].ID},
		{UUID: "link-bob-selected", Name: "Bob selected", FileID: files[3].ID, UserID: users[1].ID},
		{UUID: "link-carol", Name: "Carol", FileID: files[4].ID, UserID: users[2].ID},
		{UUID: "link-carol-unavailable", Name: "Carol unavailable", FileID: files[5].ID, UserID: users[2].ID},
		{UUID: "link-deleted-shared", Name: "Deleted shared", FileID: files[0].ID, UserID: users[3].ID},
	}
	if err := db.Create(&links).Error; err != nil {
		t.Fatal(err)
	}
	runtime := background.New(db, background.Options{})
	service := NewService(&app.Deps{DB: db, Storage: storageService, Background: runtime})
	accountResults, err := service.ListStorageMigrationAccounts(context.Background(), "ali", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(accountResults) != 1 || accountResults[0].ID != users[0].ID || accountResults[0].Username != "alice" {
		t.Fatalf("unexpected account search results: %#v", accountResults)
	}
	accountResults, err = service.ListStorageMigrationAccounts(context.Background(), "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(accountResults) != 3 {
		t.Fatalf("deleted accounts were returned by account search: %#v", accountResults)
	}
	input := StorageMigrationInput{
		SourcePoolID: sourcePool.ID, DestinationPoolID: destinationPool.ID,
		AccountIDs: []uint{users[1].ID, users[0].ID, users[0].ID},
	}
	preview, err := service.PreviewStorageMigration(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Scope != models.StorageMigrationScopeAccounts || preview.FileCount != 4 || preview.PlannedBytes != 280 || preview.SharedFileCount != 1 {
		t.Fatalf("unexpected account-scoped preview: %#v", preview)
	}
	if len(preview.Accounts) != 2 || preview.Accounts[0].ID != users[0].ID || preview.Accounts[1].ID != users[1].ID {
		t.Fatalf("account selection was not normalized: %#v", preview.Accounts)
	}
	if !strings.Contains(strings.Join(preview.Warnings, " "), "shared physical files move once for everyone") {
		t.Fatalf("shared-file consequence was not disclosed: %#v", preview.Warnings)
	}

	// A source file unavailable only to an unselected account does not block
	// this scoped migration, but the same full-pool migration remains blocked.
	if _, err := service.PreviewStorageMigration(context.Background(), StorageMigrationInput{
		SourcePoolID: sourcePool.ID, DestinationPoolID: destinationPool.ID,
	}); !errors.Is(err, ErrStorageMigrationUnavailable) {
		t.Fatalf("expected full migration to see unrelated unavailable video, got %v", err)
	}
	selectedUnavailable := models.File{UUID: "account-alice-unavailable", StorageID: mounts[0].UUID, StorageState: models.FileStorageUnavailable, Size: 5}
	if err := db.Create(&selectedUnavailable).Error; err != nil {
		t.Fatal(err)
	}
	selectedUnavailableLink := models.Link{UUID: "link-alice-unavailable", Name: "Alice unavailable", FileID: selectedUnavailable.ID, UserID: users[0].ID}
	if err := db.Create(&selectedUnavailableLink).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.PreviewStorageMigration(context.Background(), input); !errors.Is(err, ErrStorageMigrationUnavailable) {
		t.Fatalf("expected selected unavailable video to block migration, got %v", err)
	}
	if err := db.Unscoped().Delete(&selectedUnavailableLink).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Unscoped().Delete(&selectedUnavailable).Error; err != nil {
		t.Fatal(err)
	}

	// Reservations are checked only for selected physical files, so unrelated
	// account migrations can run concurrently without weakening overlap safety.
	reservation := models.StorageMigrationItem{MigrationID: 999, FileID: files[4].ID, FileUUID: files[4].UUID, ReservationKey: fmt.Sprintf("file:%d", files[4].ID)}
	if err := db.Create(&reservation).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.PreviewStorageMigration(context.Background(), input); err != nil {
		t.Fatalf("unrelated reservation blocked selected accounts: %v", err)
	}
	if _, err := service.PreviewStorageMigration(context.Background(), StorageMigrationInput{
		SourcePoolID: sourcePool.ID, DestinationPoolID: destinationPool.ID, AccountIDs: []uint{users[2].ID},
	}); !errors.Is(err, ErrStorageMigrationUnavailable) {
		// Carol still has an unavailable source video, which is checked before
		// reservations and remains the correct safety failure.
		t.Fatalf("expected selected unavailable video to take precedence, got %v", err)
	}
	if err := db.Unscoped().Delete(&reservation).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Unscoped().Delete(&links[7]).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Unscoped().Delete(&files[5]).Error; err != nil {
		t.Fatal(err)
	}
	reservation = models.StorageMigrationItem{MigrationID: 999, FileID: files[4].ID, FileUUID: files[4].UUID, ReservationKey: fmt.Sprintf("file:%d", files[4].ID)}
	if err := db.Create(&reservation).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := service.PreviewStorageMigration(context.Background(), StorageMigrationInput{
		SourcePoolID: sourcePool.ID, DestinationPoolID: destinationPool.ID, AccountIDs: []uint{users[2].ID},
	}); !errors.Is(err, ErrStorageMigrationConflict) {
		t.Fatalf("expected overlapping selected reservation conflict, got %v", err)
	}
	if err := db.Unscoped().Delete(&reservation).Error; err != nil {
		t.Fatal(err)
	}

	alicePreview, err := service.PreviewStorageMigration(context.Background(), StorageMigrationInput{
		SourcePoolID: sourcePool.ID, DestinationPoolID: destinationPool.ID, AccountIDs: []uint{users[0].ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if alicePreview.PlanFingerprint == preview.PlanFingerprint {
		t.Fatal("different account scopes produced the same plan fingerprint")
	}
	input.PlanFingerprint = preview.PlanFingerprint
	input.IdempotencyKey = "account-migration-request"
	if err := db.Model(&users[0]).Update("username", "alice-renamed").Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.StartStorageMigration(context.Background(), input, 9, "admin"); !errors.Is(err, ErrStorageMigrationConflict) {
		t.Fatalf("expected renamed account snapshot to require another review, got %v", err)
	}
	if err := db.Model(&users[0]).Update("username", "alice").Error; err != nil {
		t.Fatal(err)
	}
	migration, _, err := service.StartStorageMigration(context.Background(), input, 9, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if migration.Scope != models.StorageMigrationScopeAccounts || migration.AccountCount != 2 || migration.SharedFileCount != 1 {
		t.Fatalf("migration scope summary was not persisted: %#v", migration)
	}
	detail, err := service.GetStorageMigration(context.Background(), migration.UUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Accounts) != 2 || detail.Accounts[0].Username != "alice" || detail.Accounts[1].Username != "bob" {
		t.Fatalf("account snapshot was not returned: %#v", detail.Accounts)
	}
	if err := db.Model(&users[0]).Update("username", "alice-renamed").Error; err != nil {
		t.Fatal(err)
	}
	detail, err = service.GetStorageMigration(context.Background(), migration.UUID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Accounts[0].Username != "alice" {
		t.Fatalf("migration audit snapshot changed after account rename: %#v", detail.Accounts)
	}
	if _, err := service.PreviewStorageMigration(context.Background(), StorageMigrationInput{
		SourcePoolID: sourcePool.ID, DestinationPoolID: destinationPool.ID, AccountIDs: []uint{users[3].ID},
	}); !errors.Is(err, ErrStorageMigrationAccounts) {
		t.Fatalf("expected deleted account selection to fail, got %v", err)
	}
	tooMany := make([]uint, maxStorageMigrationAccounts+1)
	for index := range tooMany {
		tooMany[index] = uint(index + 1)
	}
	if _, err := service.PreviewStorageMigration(context.Background(), StorageMigrationInput{
		SourcePoolID: sourcePool.ID, DestinationPoolID: destinationPool.ID, AccountIDs: tooMany,
	}); !errors.Is(err, ErrStorageMigrationAccounts) {
		t.Fatalf("expected oversized account selection to fail, got %v", err)
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
	if err := db.AutoMigrate(&models.StorageMigration{}, &models.StorageMigrationAccount{}, &models.StorageMigrationItem{}); err != nil {
		t.Fatal(err)
	}
	migration := models.StorageMigration{UUID: "keep-originals", Status: models.StorageMigrationCleaningOriginals, FileCount: 3}
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
	deleted := models.StorageMigrationItem{MigrationID: migration.ID, FileID: 44, FileUUID: "deleted", Status: models.StorageMigrationItemDeleted, ReservationKey: "file:44"}
	if err := db.Create(&deleted).Error; err != nil {
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
		if kept.Status != models.StorageMigrationOriginalsRetained || kept.CleanedCount != 1 || kept.DeletedCount != 1 {
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
	if err := db.First(&deleted, deleted.ID).Error; err != nil {
		t.Fatal(err)
	}
	if deleted.Status != models.StorageMigrationItemDeleted || deleted.ReservationKey == "" {
		t.Fatalf("deleted video cleanup ownership was released: %#v", deleted)
	}
}

func TestCanceledStorageMigrationProtectsMountsDuringSafetyWindow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.StorageMigration{}, &models.StorageMigrationAccount{}, &models.StorageMigrationItem{}); err != nil {
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
	if err := db.AutoMigrate(&models.StorageMigration{}, &models.StorageMigrationAccount{}, &models.StorageMigrationItem{}); err != nil {
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
