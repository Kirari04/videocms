package services

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
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

func TestStorageMigrationBackgroundJobCutsOverAndSchedulesDeferredCleanup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.File{}, &models.Quality{}, &models.Audio{}, &models.Subtitle{}, &models.StorageMigration{}, &models.StorageMigrationAccount{}, &models.StorageMigrationItem{}, &models.StorageMigrationObject{}); err != nil {
		t.Fatal(err)
	}
	if err := background.Migrate(db); err != nil {
		t.Fatal(err)
	}
	source, _ := storage.NewLocalStore(t.TempDir())
	destination, _ := storage.NewLocalStore(t.TempDir())
	storageService, err := storage.NewService("source", storage.LegacyMediaLayout{}, map[string]storage.Store{"source": source, "destination": destination})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageService.Close() })
	file := models.File{UUID: "550e8400-e29b-41d4-a716-446655440200", StorageID: "source", StorageState: models.FileStorageAvailable, Size: 5}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	sourceKey := migrationTestKey(t, file.UUID+"/source/original.mp4")
	putMigrationTestObject(t, source, sourceKey, "media")
	migration := models.StorageMigration{UUID: "background-migration", SourcePoolName: "Source", DestinationPoolName: "Destination", Status: models.StorageMigrationQueued, FileCount: 1, PlannedBytes: 5}
	if err := db.Create(&migration).Error; err != nil {
		t.Fatal(err)
	}
	item := models.StorageMigrationItem{MigrationID: migration.ID, FileID: file.ID, FileUUID: file.UUID, SourceMountID: "source", DestinationMountID: "destination", Status: models.StorageMigrationItemPending, ReservationKey: fmt.Sprintf("file:%d", file.ID), PlannedBytes: 5}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	deps := &app.Deps{DB: db, Storage: storageService}
	worker := NewWorkerGroup(deps, nil)
	runtime := background.New(db, background.Options{PollInterval: 5 * time.Millisecond, Capacity: func(string) int { return 1 }})
	deps.Background = runtime
	if err := runtime.Register(taskStorageMigration, worker.storageMigrationHandler(runtime)); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Register(taskStorageCleanup, worker.storageMigrationCleanupHandler); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); runtime.Stop(2 * time.Second) })
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	job, _, err := runtime.Enqueue(context.Background(), background.JobSpec{
		Kind: "storage.migration", Visibility: background.VisibilityAdmin, SubjectType: "storage_migration", SubjectID: migration.UUID,
		Tasks: []background.TaskSpec{{Kind: taskStorageMigration, Queue: background.QueueStorage, Payload: storageMigrationTaskPayload{MigrationID: migration.ID}, Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&migration).Update("background_job_id", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	waitStorageMigrationJob(t, runtime, job.ID, background.JobSucceeded)
	if err := db.First(&migration, migration.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migration.Status != models.StorageMigrationRetainingOriginals || migration.CleanupJobID == "" || migration.CleanupAfter == nil {
		t.Fatalf("cleanup was not scheduled: %#v", migration)
	}
	if delay := time.Until(*migration.CleanupAfter); delay < 23*time.Hour || delay > 25*time.Hour {
		t.Fatalf("cleanup delay = %s", delay)
	}
	cleanup, err := runtime.Job(context.Background(), migration.CleanupJobID, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if cleanup.Status != background.JobQueued || len(cleanup.Tasks) != 1 || cleanup.Tasks[0].RunAfter == nil {
		t.Fatalf("unexpected cleanup job: %#v", cleanup)
	}
	if _, err := source.Stat(context.Background(), sourceKey); err != nil {
		t.Fatalf("source object was not retained before cleanup override: %v", err)
	}
	if _, err := worker.logic.StartStorageMigrationCleanupNow(context.Background(), migration.UUID, 7, "admin"); err != nil {
		t.Fatalf("start cleanup now: %v", err)
	}
	waitStorageMigrationJob(t, runtime, migration.CleanupJobID, background.JobSucceeded)
	cleanup, err = runtime.Job(context.Background(), migration.CleanupJobID, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.First(&migration, migration.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migration.Status != models.StorageMigrationCompleted || migration.CompletedAt == nil {
		t.Fatalf("immediate cleanup did not complete migration: %#v", migration)
	}
	if _, err := source.Stat(context.Background(), sourceKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("source object remained after immediate cleanup: %v", err)
	}
	foundReleaseEvent := false
	for _, event := range cleanup.Events {
		if event.Type == "job_run_requested" && event.ActorName == "admin" {
			foundReleaseEvent = true
			break
		}
	}
	if !foundReleaseEvent {
		t.Fatalf("immediate cleanup override was not recorded: %#v", cleanup.Events)
	}
	if _, err := worker.logic.StartStorageMigrationCleanupNow(context.Background(), migration.UUID, 7, "admin"); err == nil {
		t.Fatal("completed cleanup override was accepted a second time")
	}
}

func TestStorageMigrationCopiesFinalChangesBeforeAtomicCutover(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.File{}, &models.Quality{}, &models.Audio{}, &models.Subtitle{}, &models.StorageMigration{}, &models.StorageMigrationAccount{}, &models.StorageMigrationItem{}, &models.StorageMigrationObject{}); err != nil {
		t.Fatal(err)
	}
	source, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	destinationLocal, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := models.File{UUID: "550e8400-e29b-41d4-a716-446655440201", StorageID: "source", StorageState: models.FileStorageAvailable, Size: 12}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	prefix, err := storage.LegacyMediaLayout{}.FilePrefix(file.UUID)
	if err != nil {
		t.Fatal(err)
	}
	firstKey := migrationTestKey(t, prefix.String()+"/720p/segment.ts")
	lateKey := migrationTestKey(t, prefix.String()+"/720p/index.m3u8")
	putMigrationTestObject(t, source, firstKey, "first-segment")
	destination := &migrationHookStore{Store: destinationLocal, hook: func() {
		putMigrationTestObject(t, source, lateKey, "late-manifest")
	}}
	storageService, err := storage.NewService("source", storage.LegacyMediaLayout{}, map[string]storage.Store{"source": source, "destination": destination})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageService.Close() })
	worker := NewWorkerGroup(&app.Deps{DB: db, Storage: storageService}, nil)
	migration := models.StorageMigration{UUID: "migration", Status: models.StorageMigrationRunning, FileCount: 1, DestinationPoolID: 99}
	if err := db.Create(&migration).Error; err != nil {
		t.Fatal(err)
	}
	item := models.StorageMigrationItem{
		MigrationID: migration.ID, FileID: file.ID, FileUUID: file.UUID,
		SourceMountID: "source", DestinationMountID: "destination",
		Status: models.StorageMigrationItemPending, ReservationKey: "file:1", PlannedBytes: file.Size,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	if err := worker.migrateStorageItem(context.Background(), migration, &item); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&file, file.ID).Error; err != nil {
		t.Fatal(err)
	}
	if file.StorageID != "destination" {
		t.Fatalf("storage id = %q, want destination", file.StorageID)
	}
	if file.StoragePoolID == nil || *file.StoragePoolID != migration.DestinationPoolID {
		t.Fatalf("storage pool id = %v, want %d", file.StoragePoolID, migration.DestinationPoolID)
	}
	if err := db.First(&item, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != models.StorageMigrationItemCleanupPending || item.CutoverAt == nil || item.ObjectsVerified != 2 {
		t.Fatalf("unexpected migrated item: %#v", item)
	}
	assertMigrationTestObject(t, destination, firstKey, "first-segment")
	assertMigrationTestObject(t, destination, lateKey, "late-manifest")
	assertMigrationTestObject(t, source, firstKey, "first-segment")

	if err := worker.cleanupStorageMigrationItem(context.Background(), &item); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Stat(context.Background(), firstKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("source remained after cleanup: %v", err)
	}
	if err := db.First(&item, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != models.StorageMigrationItemCleaned || item.ReservationKey != "" {
		t.Fatalf("unexpected cleaned item: %#v", item)
	}
}

func TestStorageMigrationRetriesObjectsInPlaceAndCopiesSegmentsConcurrently(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.File{}, &models.StorageMigration{}, &models.StorageMigrationAccount{}, &models.StorageMigrationItem{}, &models.StorageMigrationObject{}); err != nil {
		t.Fatal(err)
	}
	source, _ := storage.NewLocalStore(t.TempDir())
	destinationLocal, _ := storage.NewLocalStore(t.TempDir())
	file := models.File{UUID: "550e8400-e29b-41d4-a716-446655440207", StorageID: "source", StorageState: models.FileStorageAvailable}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	prefix, err := storage.LegacyMediaLayout{}.FilePrefix(file.UUID)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]storage.Key, 0, 13)
	for index := 0; index < 12; index++ {
		key := migrationTestKey(t, fmt.Sprintf("%s/1080p/out%03d.ts", prefix.String(), index))
		putMigrationTestObject(t, source, key, fmt.Sprintf("segment-%03d", index))
		keys = append(keys, key)
	}
	manifest := migrationTestKey(t, prefix.String()+"/1080p/index.m3u8")
	putMigrationTestObject(t, source, manifest, "manifest")
	keys = append(keys, manifest)
	flakyKey := keys[5].String()
	destination := &migrationObservedPutStore{
		Store: destinationLocal, delay: 20 * time.Millisecond,
		failures: map[string]int{flakyKey: 2}, attempts: make(map[string]int),
	}
	service, err := storage.NewService("source", storage.LegacyMediaLayout{}, map[string]storage.Store{"source": source, "destination": destination})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	migration := models.StorageMigration{UUID: "parallel-object-copy", Status: models.StorageMigrationRunning, FileCount: 1}
	if err := db.Create(&migration).Error; err != nil {
		t.Fatal(err)
	}
	item := models.StorageMigrationItem{
		MigrationID: migration.ID, FileID: file.ID, FileUUID: file.UUID,
		SourceMountID: "source", DestinationMountID: "destination",
		Status: models.StorageMigrationItemPending, ReservationKey: fmt.Sprintf("file:%d", file.ID),
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	worker := NewWorkerGroup(&app.Deps{DB: db, Storage: service}, nil)
	if err := worker.migrateStorageItem(context.Background(), migration, &item); err != nil {
		t.Fatal(err)
	}
	if attempts := destination.attemptCount(flakyKey); attempts != 3 {
		t.Fatalf("flaky object attempts = %d, want 3", attempts)
	}
	if maximum := destination.maximumConcurrency(); maximum < 2 || maximum > storageMigrationObjectConcurrency {
		t.Fatalf("object copy concurrency = %d, want 2..%d", maximum, storageMigrationObjectConcurrency)
	}
	if destination.manifestOverlappedSegments() {
		t.Fatal("manifest upload started before all segment uploads completed")
	}
	if err := db.First(&file, file.ID).Error; err != nil {
		t.Fatal(err)
	}
	if file.StorageID != "destination" {
		t.Fatalf("file remained on %q", file.StorageID)
	}
	var checkpointCount int64
	if err := db.Model(&models.StorageMigrationObject{}).Where("item_id = ?", item.ID).Count(&checkpointCount).Error; err != nil {
		t.Fatal(err)
	}
	if checkpointCount != 0 {
		t.Fatalf("%d checkpoint(s) remained after cutover", checkpointCount)
	}
}

func TestStorageMigrationReusesDurableObjectCheckpoints(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.File{}, &models.StorageMigration{}, &models.StorageMigrationAccount{}, &models.StorageMigrationItem{}, &models.StorageMigrationObject{}); err != nil {
		t.Fatal(err)
	}
	source, _ := storage.NewLocalStore(t.TempDir())
	destinationLocal, _ := storage.NewLocalStore(t.TempDir())
	file := models.File{UUID: "550e8400-e29b-41d4-a716-446655440208", StorageID: "source", StorageState: models.FileStorageAvailable}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	first := migrationTestKey(t, file.UUID+"/720p/out000.ts")
	second := migrationTestKey(t, file.UUID+"/720p/out001.ts")
	putMigrationTestObject(t, source, first, "first")
	putMigrationTestObject(t, source, second, "second")
	putMigrationTestObject(t, destinationLocal, first, "first")
	firstInfo, err := source.Stat(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	destination := &migrationObservedPutStore{Store: destinationLocal, attempts: make(map[string]int), failures: make(map[string]int)}
	service, err := storage.NewService("source", storage.LegacyMediaLayout{}, map[string]storage.Store{"source": source, "destination": destination})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	migration := models.StorageMigration{UUID: "checkpoint-resume", Status: models.StorageMigrationRunning, FileCount: 1}
	if err := db.Create(&migration).Error; err != nil {
		t.Fatal(err)
	}
	item := models.StorageMigrationItem{
		MigrationID: migration.ID, FileID: file.ID, FileUUID: file.UUID,
		SourceMountID: "source", DestinationMountID: "destination", DestinationOwned: true,
		Status: models.StorageMigrationItemCopying, ReservationKey: fmt.Sprintf("file:%d", file.ID),
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	checkpoint := models.StorageMigrationObject{
		ItemID: item.ID, ObjectKey: first.String(), SourceRevision: storageMigrationObjectRevision(firstInfo),
		SourceSize: firstInfo.Size, Checksum: fmt.Sprintf("%x", sha256.Sum256([]byte("first"))), UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(&checkpoint).Error; err != nil {
		t.Fatal(err)
	}
	worker := NewWorkerGroup(&app.Deps{DB: db, Storage: service}, nil)
	if err := worker.copyStorageMigrationPrefix(context.Background(), &item, file, false); err != nil {
		t.Fatal(err)
	}
	if attempts := destination.attemptCount(first.String()); attempts != 0 {
		t.Fatalf("checkpointed object was uploaded %d time(s)", attempts)
	}
	if attempts := destination.attemptCount(second.String()); attempts != 1 {
		t.Fatalf("new object uploads = %d, want 1", attempts)
	}
	if err := db.First(&item, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if item.ObjectsVerified != 2 || item.BytesCopied != int64(len("first")+len("second")) {
		t.Fatalf("unexpected resumed progress: %#v", item)
	}
	var checkpointCount int64
	if err := db.Model(&models.StorageMigrationObject{}).Where("item_id = ?", item.ID).Count(&checkpointCount).Error; err != nil {
		t.Fatal(err)
	}
	if checkpointCount != 2 {
		t.Fatalf("checkpoint count = %d, want 2", checkpointCount)
	}

	// A same-size destination corruption must not be trusted when the store has
	// no ETag. The saved checksum makes the final pass repair it.
	putMigrationTestObject(t, destinationLocal, first, "wrong")
	if err := worker.copyStorageMigrationPrefix(context.Background(), &item, file, true); err != nil {
		t.Fatal(err)
	}
	if attempts := destination.attemptCount(first.String()); attempts != 1 {
		t.Fatalf("corrupted checkpointed object uploads = %d, want 1", attempts)
	}
	assertMigrationTestObject(t, destination, first, "first")

	// A same-size source replacement must not be mistaken for the already
	// copied object either. The final pass compares its source revision.
	time.Sleep(time.Millisecond)
	putMigrationTestObject(t, source, first, "fresh")
	if err := worker.copyStorageMigrationPrefix(context.Background(), &item, file, true); err != nil {
		t.Fatal(err)
	}
	if attempts := destination.attemptCount(first.String()); attempts != 2 {
		t.Fatalf("changed checkpointed object uploads = %d, want 2 total", attempts)
	}
	if attempts := destination.attemptCount(second.String()); attempts != 1 {
		t.Fatalf("unchanged checkpointed object uploads = %d, want 1 total", attempts)
	}
	assertMigrationTestObject(t, destination, first, "fresh")
}

func TestStorageMigrationCleanupWaitsForActiveEncoding(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.File{}, &models.Quality{}, &models.Audio{}, &models.Subtitle{},
		&models.StorageMigration{}, &models.StorageMigrationAccount{}, &models.StorageMigrationItem{},
	); err != nil {
		t.Fatal(err)
	}
	source, _ := storage.NewLocalStore(t.TempDir())
	destination, _ := storage.NewLocalStore(t.TempDir())
	service, err := storage.NewService("source", storage.LegacyMediaLayout{}, map[string]storage.Store{"source": source, "destination": destination})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	file := models.File{UUID: "550e8400-e29b-41d4-a716-446655440206", StorageID: "destination", StorageState: models.FileStorageAvailable}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	quality := models.Quality{FileID: file.ID, Name: "720p", Encoding: true}
	if err := db.Create(&quality).Error; err != nil {
		t.Fatal(err)
	}
	key := migrationTestKey(t, file.UUID+"/source/original.mp4")
	putMigrationTestObject(t, source, key, "media")
	migration := models.StorageMigration{UUID: "encoding-cleanup-wait", Status: models.StorageMigrationRetainingOriginals, FileCount: 1}
	if err := db.Create(&migration).Error; err != nil {
		t.Fatal(err)
	}
	item := models.StorageMigrationItem{
		MigrationID: migration.ID, FileID: file.ID, FileUUID: file.UUID,
		SourceMountID: "source", DestinationMountID: "destination",
		Status: models.StorageMigrationItemCleanupPending, ReservationKey: fmt.Sprintf("file:%d", file.ID),
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	worker := NewWorkerGroup(&app.Deps{DB: db, Storage: service}, nil)
	handler := worker.storageMigrationCleanupHandler
	task := background.Task{PayloadVersion: 1, Payload: fmt.Sprintf(`{"migrationId":%d}`, migration.ID)}
	if _, err := handler(context.Background(), task); err == nil {
		t.Fatal("cleanup proceeded while encoding was active")
	} else {
		var taskErr *background.TaskError
		if !errors.As(err, &taskErr) || taskErr.Class != background.ErrorDeferred || taskErr.Code != "encoding_active" {
			t.Fatalf("cleanup returned %T %v, want an encoding deferral", err, err)
		}
	}
	assertMigrationTestObject(t, source, key, "media")
	if err := db.First(&item, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != models.StorageMigrationItemCleanupPending || item.ProgressMessage != "Waiting for video encoding to finish" {
		t.Fatalf("unexpected waiting item state: %#v", item)
	}
	if err := db.First(&migration, migration.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migration.Status != models.StorageMigrationCleaningOriginals || migration.Phase != "Waiting for video encoding to finish" {
		t.Fatalf("unexpected waiting migration state: %#v", migration)
	}

	if err := db.Model(&quality).Update("encoding", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := handler(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Stat(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("source remained after encoding completed: %v", err)
	}
	if err := db.First(&item, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != models.StorageMigrationItemCleaned || item.ReservationKey != "" {
		t.Fatalf("unexpected cleaned item: %#v", item)
	}
}

func TestStorageMigrationDeletionBetweenCopyAndCutoverIsTerminal(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.File{}, &models.StorageMigration{}, &models.StorageMigrationAccount{}, &models.StorageMigrationItem{}, &models.StorageMigrationObject{}); err != nil {
		t.Fatal(err)
	}
	source, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	destinationLocal, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := models.File{UUID: "550e8400-e29b-41d4-a716-446655440204", StorageID: "source", StorageState: models.FileStorageAvailable, Size: 5}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	key := migrationTestKey(t, file.UUID+"/source/original.mp4")
	putMigrationTestObject(t, source, key, "media")
	destination := &migrationHookStore{Store: destinationLocal, hook: func() {
		if err := db.Delete(&file).Error; err != nil {
			t.Errorf("delete file during copy: %v", err)
		}
	}}
	service, err := storage.NewService("source", storage.LegacyMediaLayout{}, map[string]storage.Store{"source": source, "destination": destination})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	migration := models.StorageMigration{UUID: "deleted-during-copy", Status: models.StorageMigrationRunning, FileCount: 1, PlannedBytes: 5}
	if err := db.Create(&migration).Error; err != nil {
		t.Fatal(err)
	}
	item := models.StorageMigrationItem{
		MigrationID: migration.ID, FileID: file.ID, FileUUID: file.UUID,
		SourceMountID: "source", DestinationMountID: "destination", Status: models.StorageMigrationItemPending,
		ReservationKey: fmt.Sprintf("file:%d", file.ID), PlannedBytes: 5,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	worker := NewWorkerGroup(&app.Deps{DB: db, Storage: service}, nil)
	if err := worker.migrateStorageItem(context.Background(), migration, &item); err != nil {
		t.Fatalf("deleted video failed migration: %v", err)
	}
	if err := db.First(&item, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != models.StorageMigrationItemDeleted || item.CutoverAt != nil || item.DestinationOwned {
		t.Fatalf("unexpected deleted migration item: %#v", item)
	}
	if item.ReservationKey == "" {
		t.Fatal("reservation was released before the file deleter removed the source")
	}
	var deletedFile models.File
	if err := db.Unscoped().First(&deletedFile, file.ID).Error; err != nil {
		t.Fatal(err)
	}
	if deletedFile.StorageID != "source" {
		t.Fatalf("deleted file cut over to %q", deletedFile.StorageID)
	}
	assertMigrationTestObject(t, source, key, "media")
	if _, err := destination.Stat(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("partial destination remained: %v", err)
	}
	if err := worker.refreshStorageMigrationProgress(context.Background(), migration.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&migration, migration.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migration.DeletedCount != 1 || migration.CutoverCount != 0 || migration.ActualBytes != 0 || migration.CopiedBytes != 0 {
		t.Fatalf("unexpected deletion-aware progress: %#v", migration)
	}
}

func TestStorageMigrationCleanupSkipsDeletedVideo(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.StorageMigrationItem{}, &models.StorageMigrationObject{}); err != nil {
		t.Fatal(err)
	}
	source, _ := storage.NewLocalStore(t.TempDir())
	destination, _ := storage.NewLocalStore(t.TempDir())
	service, err := storage.NewService("source", storage.LegacyMediaLayout{}, map[string]storage.Store{"source": source, "destination": destination})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	item := models.StorageMigrationItem{FileID: 42, FileUUID: "550e8400-e29b-41d4-a716-446655440205", SourceMountID: "source", DestinationMountID: "destination", Status: models.StorageMigrationItemDeleted, ReservationKey: "file:42"}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	key := migrationTestKey(t, item.FileUUID+"/source/original.mp4")
	putMigrationTestObject(t, source, key, "awaiting-deleter")
	putMigrationTestObject(t, destination, key, "late-copy")
	worker := NewWorkerGroup(&app.Deps{DB: db, Storage: service}, nil)
	if err := worker.cleanupStorageMigrationItem(context.Background(), &item); err != nil {
		t.Fatal(err)
	}
	if err := worker.cleanupCanceledStorageMigrationItem(context.Background(), &item); err != nil {
		t.Fatal(err)
	}
	assertMigrationTestObject(t, source, key, "awaiting-deleter")
	if err := db.First(&item, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != models.StorageMigrationItemDeleted || item.ReservationKey == "" {
		t.Fatalf("cleanup changed deletion ownership: %#v", item)
	}
	if err := worker.cleanupDeletedStorageMigrationDestination(context.Background(), &item, true); err != nil {
		t.Fatal(err)
	}
	if _, err := destination.Stat(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("late destination copy remained after deletion finalized: %v", err)
	}
}

func TestStorageMigrationCleanupStopsOnlyBetweenVideos(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.File{}, &models.Quality{}, &models.Audio{}, &models.Subtitle{}, &models.StorageMigration{}, &models.StorageMigrationAccount{}, &models.StorageMigrationItem{}, &models.StorageMigrationObject{}); err != nil {
		t.Fatal(err)
	}
	sourceLocal, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	destination, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := &migrationBlockingDeleteStore{Store: sourceLocal, started: make(chan struct{}), release: make(chan struct{})}
	service, err := storage.NewService("source", storage.LegacyMediaLayout{}, map[string]storage.Store{"source": source, "destination": destination})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	file := models.File{UUID: "550e8400-e29b-41d4-a716-446655440203", StorageID: "destination", StorageState: models.FileStorageAvailable}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	first := migrationTestKey(t, file.UUID+"/source/first.ts")
	second := migrationTestKey(t, file.UUID+"/source/second.m3u8")
	putMigrationTestObject(t, source, first, "first")
	putMigrationTestObject(t, source, second, "second")
	migration := models.StorageMigration{UUID: "cleanup-checkpoint", Status: models.StorageMigrationCleaningOriginals, FileCount: 1}
	if err := db.Create(&migration).Error; err != nil {
		t.Fatal(err)
	}
	item := models.StorageMigrationItem{
		MigrationID: migration.ID, FileID: file.ID, FileUUID: file.UUID, SourceMountID: "source", DestinationMountID: "destination",
		Status: models.StorageMigrationItemCleanupPending, ReservationKey: fmt.Sprintf("file:%d", file.ID),
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	worker := NewWorkerGroup(&app.Deps{DB: db, Storage: service}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- worker.cleanupStorageMigrationItem(ctx, &item) }()
	select {
	case <-source.started:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup did not begin")
	}
	cancel()
	select {
	case err := <-result:
		t.Fatalf("cleanup stopped halfway through a video: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(source.release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup did not finish the current video")
	}
	for _, key := range []storage.Key{first, second} {
		if _, err := source.Stat(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("source object %s remained after cleanup checkpoint: %v", key.String(), err)
		}
	}
	if err := db.First(&item, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != models.StorageMigrationItemCleaned || item.ReservationKey != "" {
		t.Fatalf("cleanup checkpoint was not committed: %#v", item)
	}
}

func TestStorageMigrationRefusesUnownedDestinationPrefix(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.File{}, &models.StorageMigration{}, &models.StorageMigrationAccount{}, &models.StorageMigrationItem{}, &models.StorageMigrationObject{}); err != nil {
		t.Fatal(err)
	}
	source, _ := storage.NewLocalStore(t.TempDir())
	destination, _ := storage.NewLocalStore(t.TempDir())
	service, err := storage.NewService("source", storage.LegacyMediaLayout{}, map[string]storage.Store{"source": source, "destination": destination})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	file := models.File{UUID: "550e8400-e29b-41d4-a716-446655440202", StorageID: "source", StorageState: models.FileStorageAvailable}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	key := migrationTestKey(t, file.UUID+"/source/original.mp4")
	putMigrationTestObject(t, source, key, "source")
	putMigrationTestObject(t, destination, key, "unrelated")
	migration := models.StorageMigration{UUID: "migration-conflict", Status: models.StorageMigrationRunning, FileCount: 1}
	db.Create(&migration)
	item := models.StorageMigrationItem{MigrationID: migration.ID, FileID: file.ID, FileUUID: file.UUID, SourceMountID: "source", DestinationMountID: "destination", Status: models.StorageMigrationItemPending, ReservationKey: "file:conflict"}
	db.Create(&item)
	worker := NewWorkerGroup(&app.Deps{DB: db, Storage: service}, nil)
	if err := worker.migrateStorageItem(context.Background(), migration, &item); !errors.Is(err, errStorageMigrationStateConflict) {
		t.Fatalf("expected destination conflict, got %v", err)
	}
	if err := db.First(&file, file.ID).Error; err != nil {
		t.Fatal(err)
	}
	if file.StorageID != "source" {
		t.Fatalf("conflicted migration cut over to %q", file.StorageID)
	}
}

func TestCanceledStorageMigrationRemovesOnlyUnreferencedDestinationData(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.File{}, &models.StorageMigration{}, &models.StorageMigrationAccount{}, &models.StorageMigrationItem{}, &models.StorageMigrationObject{}); err != nil {
		t.Fatal(err)
	}
	source, _ := storage.NewLocalStore(t.TempDir())
	destination, _ := storage.NewLocalStore(t.TempDir())
	service, _ := storage.NewService("source", storage.LegacyMediaLayout{}, map[string]storage.Store{"source": source, "destination": destination})
	t.Cleanup(func() { _ = service.Close() })
	worker := NewWorkerGroup(&app.Deps{DB: db, Storage: service}, nil)
	migration := models.StorageMigration{UUID: "canceled", Status: models.StorageMigrationCanceled, FileCount: 2}
	db.Create(&migration)

	pendingFile := models.File{UUID: "550e8400-e29b-41d4-a716-446655440401", StorageID: "source", StorageState: models.FileStorageAvailable}
	db.Create(&pendingFile)
	pendingKey := migrationTestKey(t, pendingFile.UUID+"/source/original.mp4")
	putMigrationTestObject(t, source, pendingKey, "source")
	putMigrationTestObject(t, destination, pendingKey, "partial")
	pending := models.StorageMigrationItem{MigrationID: migration.ID, FileID: pendingFile.ID, FileUUID: pendingFile.UUID, SourceMountID: "source", DestinationMountID: "destination", Status: models.StorageMigrationItemCopying, ReservationKey: fmt.Sprintf("file:%d", pendingFile.ID), DestinationOwned: true}
	db.Create(&pending)
	if err := db.Create(&models.StorageMigrationObject{ItemID: pending.ID, ObjectKey: pendingKey.String(), SourceRevision: "pending"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := worker.cleanupCanceledStorageMigrationItem(context.Background(), &pending); err != nil {
		t.Fatal(err)
	}
	if _, err := destination.Stat(context.Background(), pendingKey); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("partial destination remained: %v", err)
	}
	assertMigrationTestObject(t, source, pendingKey, "source")
	var pendingCheckpointCount int64
	if err := db.Model(&models.StorageMigrationObject{}).Where("item_id = ?", pending.ID).Count(&pendingCheckpointCount).Error; err != nil {
		t.Fatal(err)
	}
	if pendingCheckpointCount != 0 {
		t.Fatalf("%d canceled-item checkpoint(s) remained", pendingCheckpointCount)
	}

	cutoverFile := models.File{UUID: "550e8400-e29b-41d4-a716-446655440402", StorageID: "destination", StorageState: models.FileStorageAvailable}
	db.Create(&cutoverFile)
	cutoverKey := migrationTestKey(t, cutoverFile.UUID+"/source/original.mp4")
	putMigrationTestObject(t, source, cutoverKey, "original")
	putMigrationTestObject(t, destination, cutoverKey, "active")
	now := time.Now().UTC()
	cutover := models.StorageMigrationItem{MigrationID: migration.ID, FileID: cutoverFile.ID, FileUUID: cutoverFile.UUID, SourceMountID: "source", DestinationMountID: "destination", Status: models.StorageMigrationItemCleanupPending, ReservationKey: fmt.Sprintf("file:%d", cutoverFile.ID), DestinationOwned: true, CutoverAt: &now}
	db.Create(&cutover)
	if err := worker.cleanupCanceledStorageMigrationItem(context.Background(), &cutover); err != nil {
		t.Fatal(err)
	}
	assertMigrationTestObject(t, destination, cutoverKey, "active")
	assertMigrationTestObject(t, source, cutoverKey, "original")
	if err := db.First(&cutover, cutover.ID).Error; err != nil {
		t.Fatal(err)
	}
	if cutover.Status != models.StorageMigrationItemOriginalKept || cutover.ReservationKey != "" {
		t.Fatalf("unexpected cutover cancellation state: %#v", cutover)
	}
}

func TestStorageMigrationReconciliationRepairsCanceledJobCrashWindow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.StorageMigration{}, &models.StorageMigrationAccount{}, &models.StorageMigrationItem{}, &models.StorageMigrationObject{}); err != nil {
		t.Fatal(err)
	}
	if err := background.Migrate(db); err != nil {
		t.Fatal(err)
	}
	runtime := background.New(db, background.Options{})
	deps := &app.Deps{DB: db, Background: runtime}
	worker := NewWorkerGroup(deps, nil)
	migration := models.StorageMigration{UUID: "reconcile-canceled", SourcePoolName: "Source", DestinationPoolName: "Destination", Status: models.StorageMigrationQueued, FileCount: 1}
	if err := db.Create(&migration).Error; err != nil {
		t.Fatal(err)
	}
	item := models.StorageMigrationItem{MigrationID: migration.ID, FileID: 1, FileUUID: "file", SourceMountID: "source", DestinationMountID: "destination", Status: models.StorageMigrationItemPending, ReservationKey: "file:1"}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	mainJob, _, err := runtime.Enqueue(context.Background(), background.JobSpec{
		Kind: "storage.migration", Visibility: background.VisibilityAdmin,
		SubjectType: "storage_migration", SubjectID: migration.UUID,
		Tasks: []background.TaskSpec{{Kind: taskStorageMigration, Queue: background.QueueStorage, Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.CancelJob(context.Background(), mainJob.ID, 1, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.storageMigrationReconcileHandler(runtime)(context.Background(), background.Task{}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&migration, migration.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migration.Status != models.StorageMigrationCanceled || migration.BackgroundJobID != mainJob.ID || migration.CleanupJobID == "" {
		t.Fatalf("canceled migration was not reconciled: %#v", migration)
	}
	abortJob, err := runtime.Job(context.Background(), migration.CleanupJobID, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if abortJob.Kind != "storage.migration.abort_cleanup" || abortJob.Status != background.JobQueued {
		t.Fatalf("unexpected reconciled cleanup job: %#v", abortJob)
	}

	retaining := models.StorageMigration{UUID: "reconcile-retention", Status: models.StorageMigrationRetainingOriginals, FileCount: 1}
	if err := db.Create(&retaining).Error; err != nil {
		t.Fatal(err)
	}
	retainedItem := models.StorageMigrationItem{MigrationID: retaining.ID, FileID: 2, FileUUID: "retained", SourceMountID: "source", DestinationMountID: "destination", Status: models.StorageMigrationItemCleanupPending, ReservationKey: "file:2"}
	if err := db.Create(&retainedItem).Error; err != nil {
		t.Fatal(err)
	}
	cleanupJob, _, err := runtime.Enqueue(context.Background(), background.JobSpec{
		Kind: "storage.migration.cleanup", Visibility: background.VisibilityAdmin,
		SubjectType: "storage_migration", SubjectID: retaining.UUID,
		Tasks: []background.TaskSpec{{Kind: taskStorageCleanup, Queue: background.QueueStorage, Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&retaining).Update("cleanup_job_id", cleanupJob.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := runtime.CancelJob(context.Background(), cleanupJob.ID, 1, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.storageMigrationReconcileHandler(runtime)(context.Background(), background.Task{}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&retaining, retaining.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&retainedItem, retainedItem.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retaining.Status != models.StorageMigrationOriginalsRetained || retainedItem.Status != models.StorageMigrationItemOriginalKept || retainedItem.ReservationKey != "" {
		t.Fatalf("canceled original cleanup was not reconciled: migration=%#v item=%#v", retaining, retainedItem)
	}

	missing := models.StorageMigration{UUID: "reconcile-missing-main", SourcePoolName: "Source", DestinationPoolName: "Destination", Status: models.StorageMigrationQueued, FileCount: 1}
	if err := db.Create(&missing).Error; err != nil {
		t.Fatal(err)
	}
	missingItem := models.StorageMigrationItem{MigrationID: missing.ID, FileID: 3, FileUUID: "missing", SourceMountID: "source", DestinationMountID: "destination", Status: models.StorageMigrationItemPending, ReservationKey: "file:3"}
	if err := db.Create(&missingItem).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := worker.storageMigrationReconcileHandler(runtime)(context.Background(), background.Task{}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&missing, missing.ID).Error; err != nil {
		t.Fatal(err)
	}
	if missing.BackgroundJobID == "" {
		t.Fatal("missing migration job was not recreated")
	}
	recreated, err := runtime.Job(context.Background(), missing.BackgroundJobID, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if recreated.Status != background.JobQueued || !recreated.Pausable {
		t.Fatalf("unexpected recreated migration job: %#v", recreated.Job)
	}
	if err := runtime.PauseJob(context.Background(), recreated.ID, 1, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&missing).Updates(map[string]any{"status": models.StorageMigrationRunning, "phase": "stale"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := worker.storageMigrationReconcileHandler(runtime)(context.Background(), background.Task{}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&missing, missing.ID).Error; err != nil {
		t.Fatal(err)
	}
	if missing.Status != models.StorageMigrationPaused || missing.Phase != "Migration paused" {
		t.Fatalf("paused job projection was not repaired: %#v", missing)
	}
	if err := runtime.ResumeJob(context.Background(), recreated.ID, 1, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.storageMigrationReconcileHandler(runtime)(context.Background(), background.Task{}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&missing, missing.ID).Error; err != nil {
		t.Fatal(err)
	}
	if missing.Status != models.StorageMigrationQueued {
		t.Fatalf("resumed job projection was not repaired: %#v", missing)
	}
}

type migrationHookStore struct {
	storage.Store
	once sync.Once
	hook func()
}

type migrationObservedPutStore struct {
	storage.Store
	delay time.Duration

	mu                     sync.Mutex
	attempts               map[string]int
	failures               map[string]int
	active                 int
	maxActive              int
	activeSegments         int
	manifestSegmentOverlap bool
}

func (s *migrationObservedPutStore) Put(ctx context.Context, key storage.Key, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
	isManifest := strings.HasSuffix(strings.ToLower(key.String()), ".m3u8")
	s.mu.Lock()
	s.attempts[key.String()]++
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	if isManifest {
		if s.activeSegments > 0 {
			s.manifestSegmentOverlap = true
		}
	} else {
		s.activeSegments++
	}
	shouldFail := s.failures[key.String()] > 0
	if shouldFail {
		s.failures[key.String()]--
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.active--
		if !isManifest {
			s.activeSegments--
		}
		s.mu.Unlock()
	}()
	if s.delay > 0 {
		timer := time.NewTimer(s.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return storage.ObjectInfo{}, ctx.Err()
		case <-timer.C:
		}
	}
	if shouldFail {
		return storage.ObjectInfo{}, io.ErrUnexpectedEOF
	}
	return s.Store.Put(ctx, key, source, options)
}

func (s *migrationObservedPutStore) attemptCount(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts[key]
}

func (s *migrationObservedPutStore) maximumConcurrency() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxActive
}

func (s *migrationObservedPutStore) manifestOverlappedSegments() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.manifestSegmentOverlap
}

type migrationBlockingDeleteStore struct {
	storage.Store
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (s *migrationBlockingDeleteStore) Delete(ctx context.Context, key storage.Key) error {
	s.once.Do(func() {
		close(s.started)
		<-s.release
	})
	return s.Store.Delete(ctx, key)
}

func (s *migrationHookStore) Put(ctx context.Context, key storage.Key, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
	info, err := s.Store.Put(ctx, key, source, options)
	if err == nil && s.hook != nil {
		s.once.Do(s.hook)
	}
	return info, err
}

func migrationTestKey(t *testing.T, value string) storage.Key {
	t.Helper()
	key, err := storage.ParseKey(value)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func putMigrationTestObject(t *testing.T, store storage.Store, key storage.Key, value string) {
	t.Helper()
	size := int64(len(value))
	if _, err := store.Put(context.Background(), key, strings.NewReader(value), storage.PutOptions{ExpectedSize: &size}); err != nil {
		t.Fatal(err)
	}
}

func assertMigrationTestObject(t *testing.T, store storage.Store, key storage.Key, expected string) {
	t.Helper()
	object, err := store.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer object.Body.Close()
	data, err := io.ReadAll(object.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != expected {
		t.Fatalf("object %s = %q, want %q", key.String(), data, expected)
	}
}

func waitStorageMigrationJob(t *testing.T, runtime *background.Runtime, jobID string, status string) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		detail, err := runtime.Job(context.Background(), jobID, nil, true)
		if err == nil && detail.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	detail, err := runtime.Job(context.Background(), jobID, nil, true)
	t.Fatalf("job did not reach %s: detail=%#v err=%v", status, detail, err)
}
