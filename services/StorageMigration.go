package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"ch/kirari04/videocms/background"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"

	"gorm.io/gorm"
)

const storageMigrationCleanupGrace = 24 * time.Hour

var errStorageMigrationStateConflict = errors.New("storage migration state changed")

type storageMigrationTaskPayload struct {
	MigrationID uint `json:"migrationId"`
}

func (w *WorkerGroup) storageMigrationHandler(runtime *background.Runtime) background.Handler {
	return func(ctx context.Context, task background.Task) (background.Result, error) {
		var payload storageMigrationTaskPayload
		if err := decodeTaskPayload(task, &payload); err != nil {
			return background.Result{}, err
		}
		var migration models.StorageMigration
		if err := w.deps.DB.WithContext(ctx).First(&migration, payload.MigrationID).Error; err != nil {
			return background.Result{}, background.Permanent("migration_missing", "The storage migration no longer exists", err)
		}
		if migration.CanceledAt != nil && migration.CleanupJobID != "" {
			if cleanup, cleanupErr := runtime.Job(ctx, migration.CleanupJobID, nil, true); cleanupErr == nil && !storageMigrationJobTerminal(cleanup.Status) {
				return background.Result{}, &background.TaskError{Code: "abort_cleanup_running", Public: "Canceled migration cleanup is still running", Class: background.ErrorTransient, RetryAfter: 5 * time.Second}
			}
		}
		if migration.Status == models.StorageMigrationRetainingOriginals || migration.Status == models.StorageMigrationCompleted || migration.Status == models.StorageMigrationOriginalsRetained {
			if migration.CleanupJobID == "" && migration.Status == models.StorageMigrationRetainingOriginals {
				if err := w.scheduleStorageMigrationCleanup(ctx, runtime, &migration); err != nil {
					return background.Result{}, background.Transient("cleanup_queue_failed", "Cleanup could not be scheduled", err)
				}
			}
			return background.Result{ResultType: "storage_migration", ResultID: migration.UUID, Phase: "Migration complete"}, nil
		}
		if err := w.prepareStorageMigrationRetry(ctx, &migration); err != nil {
			if errors.Is(err, errStorageMigrationStateConflict) {
				return background.Result{}, background.Permanent("migration_conflict", "A video is reserved by another migration", err)
			}
			return background.Result{}, background.Transient("migration_reservation_failed", "Migration videos could not be reserved", err)
		}
		now := time.Now().UTC()
		if err := w.deps.DB.WithContext(ctx).Model(&migration).Updates(map[string]any{
			"status": models.StorageMigrationRunning, "phase": "Migrating videos", "started_at": gorm.Expr("COALESCE(started_at, ?)", now),
			"background_job_id": task.JobID, "error_code": "", "error_message": "",
		}).Error; err != nil {
			return background.Result{}, background.Transient("migration_state_failed", "Migration state could not be updated", err)
		}

		var items []models.StorageMigrationItem
		if err := w.deps.DB.WithContext(ctx).
			Where("migration_id = ? AND status NOT IN ?", migration.ID, []string{models.StorageMigrationItemCleanupPending, models.StorageMigrationItemCleaned}).
			Order("id ASC").Find(&items).Error; err != nil {
			return background.Result{}, background.Transient("migration_items_failed", "Migration videos could not be loaded", err)
		}
		if err := w.runStorageMigrationItems(ctx, migration, items); err != nil {
			status, phase := models.StorageMigrationRunning, "A copy failed; retry will be scheduled"
			permanentFailure := errors.Is(err, errStorageMigrationStateConflict)
			if permanentFailure || task.AttemptCount >= task.MaxAttempts {
				status, phase = models.StorageMigrationFailed, "Migration needs attention"
			}
			if ctx.Err() != nil {
				status, phase = models.StorageMigrationCanceled, "Migration canceled"
				if background.PauseRequested(ctx) {
					status, phase = models.StorageMigrationPaused, "Migration paused"
				}
			}
			updates := map[string]any{"status": status, "phase": phase}
			if status == models.StorageMigrationFailed || status == models.StorageMigrationRunning {
				updates["error_code"] = "video_migration_failed"
				updates["error_message"] = boundedServiceError(err.Error())
			}
			if status == models.StorageMigrationCanceled {
				updates["canceled_at"] = time.Now().UTC()
			}
			_ = w.deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&migration).Updates(updates).Error
			if status == models.StorageMigrationCanceled && w.logic != nil {
				if abortErr := w.logic.EnsureStorageMigrationAbortCleanup(context.WithoutCancel(ctx), migration.ID); abortErr != nil {
					_ = w.deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&migration).Updates(map[string]any{
						"phase":      "Canceled; cleanup will be reconciled automatically",
						"error_code": "abort_cleanup_pending", "error_message": boundedServiceError(abortErr.Error()),
					}).Error
				}
			}
			if ctx.Err() != nil {
				return background.Result{}, ctx.Err()
			}
			if errors.Is(err, errStorageMigrationStateConflict) {
				return background.Result{}, background.Permanent("migration_conflict", "A video changed storage while it was being migrated", err)
			}
			return background.Result{}, background.Transient("storage_copy_failed", "A video could not be copied and verified", err)
		}
		if err := w.refreshStorageMigrationProgress(context.WithoutCancel(ctx), migration.ID); err != nil {
			return background.Result{}, background.Transient("migration_progress_failed", "Migration progress could not be finalized", err)
		}
		if err := ctx.Err(); err != nil {
			return background.Result{}, err
		}
		if err := w.scheduleStorageMigrationCleanup(context.WithoutCancel(ctx), runtime, &migration); err != nil {
			return background.Result{}, background.Transient("cleanup_queue_failed", "Cleanup could not be scheduled", err)
		}
		return background.Result{ResultType: "storage_migration", ResultID: migration.UUID, Phase: "Migration complete"}, nil
	}
}

func (w *WorkerGroup) prepareStorageMigrationRetry(ctx context.Context, migration *models.StorageMigration) error {
	return w.deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var items []models.StorageMigrationItem
		if err := tx.Where("migration_id = ?", migration.ID).Order("id ASC").Find(&items).Error; err != nil {
			return err
		}
		for index := range items {
			item := &items[index]
			var file models.File
			if err := tx.First(&file, item.FileID).Error; err != nil {
				return err
			}
			status := models.StorageMigrationItemPending
			if file.StorageID == item.DestinationMountID && item.CutoverAt != nil {
				status = models.StorageMigrationItemCleanupPending
			} else if file.StorageID != item.SourceMountID {
				return fmt.Errorf("%w: file %d moved to %s", errStorageMigrationStateConflict, file.ID, file.StorageID)
			}
			updates := map[string]any{"reservation_key": fmt.Sprintf("file:%d", item.FileID), "status": status, "error_code": "", "error_message": ""}
			if status == models.StorageMigrationItemPending && item.Status == models.StorageMigrationItemCanceled {
				updates["destination_owned"] = false
				updates["bytes_copied"] = 0
				updates["objects_verified"] = 0
			}
			if err := tx.Model(item).Updates(updates).Error; err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "unique") {
					return errStorageMigrationStateConflict
				}
				return err
			}
		}
		return tx.Model(migration).Updates(map[string]any{"keep_originals": false, "cleanup_job_id": "", "cleanup_after": nil, "canceled_at": nil}).Error
	})
}

func (w *WorkerGroup) runStorageMigrationItems(ctx context.Context, migration models.StorageMigration, items []models.StorageMigrationItem) error {
	if len(items) == 0 {
		return nil
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	queue := make(chan models.StorageMigrationItem, len(items))
	for _, item := range items {
		queue <- item
	}
	close(queue)
	var workers sync.WaitGroup
	var firstErr error
	var errorMu sync.Mutex
	workerCount := 2
	if len(items) < workerCount {
		workerCount = len(items)
	}
	for index := 0; index < workerCount; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range queue {
				if workCtx.Err() != nil {
					return
				}
				if err := w.migrateStorageItem(workCtx, migration, &item); err != nil {
					errorMu.Lock()
					if firstErr == nil {
						firstErr = err
						cancel()
					}
					errorMu.Unlock()
					return
				}
				_ = w.refreshStorageMigrationProgress(context.WithoutCancel(workCtx), migration.ID)
			}
		}()
	}
	workers.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return firstErr
}

func (w *WorkerGroup) migrateStorageItem(ctx context.Context, migration models.StorageMigration, item *models.StorageMigrationItem) (err error) {
	if item.Status == models.StorageMigrationItemCleanupPending || item.Status == models.StorageMigrationItemCleaned {
		return nil
	}
	now := time.Now().UTC()
	if err := w.deps.DB.WithContext(ctx).Model(item).Updates(map[string]any{
		"status": models.StorageMigrationItemCopying, "copy_started_at": gorm.Expr("COALESCE(copy_started_at, ?)", now),
		"error_code": "", "error_message": "", "progress_message": "Copying and verifying media",
	}).Error; err != nil {
		return err
	}
	defer func() {
		if err == nil {
			return
		}
		status, code, message := models.StorageMigrationItemFailed, "copy_failed", boundedServiceError(err.Error())
		if ctx.Err() != nil {
			status, code, message = models.StorageMigrationItemPending, "", "Paused at a safe checkpoint"
		}
		_ = w.deps.DB.WithContext(context.WithoutCancel(ctx)).Model(item).Updates(map[string]any{
			"status": status, "error_code": code, "error_message": message, "progress_message": message,
		}).Error
	}()

	releaseFile := w.deps.StorageLifecycle.FileReadLock(item.FileID)
	var file models.File
	if err = w.deps.DB.WithContext(ctx).First(&file, item.FileID).Error; err != nil {
		releaseFile()
		return err
	}
	if file.StorageID != item.SourceMountID || file.StorageState != models.FileStorageAvailable {
		releaseFile()
		return fmt.Errorf("%w: file %d is no longer on source mount %s", errStorageMigrationStateConflict, file.ID, item.SourceMountID)
	}
	releaseMounts := w.deps.StorageLifecycle.ReadLocks(item.SourceMountID, item.DestinationMountID)
	if err = w.copyStorageMigrationPrefix(ctx, item, file, false); err != nil {
		releaseMounts()
		releaseFile()
		return err
	}
	releaseMounts()
	releaseFile()

	if err = ctx.Err(); err != nil {
		return err
	}
	releaseFile = w.deps.StorageLifecycle.FileWriteLock(item.FileID)
	defer releaseFile()
	releaseMounts = w.deps.StorageLifecycle.ReadLocks(item.SourceMountID, item.DestinationMountID)
	defer releaseMounts()
	if err = w.deps.DB.WithContext(ctx).First(&file, item.FileID).Error; err != nil {
		return err
	}
	if file.StorageID != item.SourceMountID || file.StorageState != models.FileStorageAvailable {
		return fmt.Errorf("%w: file %d changed before cutover", errStorageMigrationStateConflict, file.ID)
	}
	if err = w.copyStorageMigrationPrefix(ctx, item, file, true); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	cutoverAt := time.Now().UTC()
	err = w.deps.DB.WithContext(context.WithoutCancel(ctx)).Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&models.File{}).
			Where("id = ? AND storage_id = ? AND storage_state = ?", file.ID, item.SourceMountID, models.FileStorageAvailable).
			Update("storage_id", item.DestinationMountID)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errStorageMigrationStateConflict
		}
		return tx.Model(item).Updates(map[string]any{
			"status": models.StorageMigrationItemCleanupPending, "verified_at": &cutoverAt, "cutover_at": &cutoverAt,
			"progress_message": "Destination active; original retained", "error_code": "", "error_message": "",
		}).Error
	})
	return err
}

func (w *WorkerGroup) copyStorageMigrationPrefix(ctx context.Context, item *models.StorageMigrationItem, file models.File, finalSync bool) error {
	source, err := w.deps.Storage.Store(item.SourceMountID)
	if err != nil {
		return err
	}
	destination, err := w.deps.Storage.Store(item.DestinationMountID)
	if err != nil {
		return err
	}
	prefix, err := w.deps.Storage.Layout().FilePrefix(file.UUID)
	if err != nil {
		return err
	}
	if !item.DestinationOwned {
		existing, err := storage.PrefixInventory(ctx, destination, prefix)
		if err != nil {
			return err
		}
		if len(existing) > 0 {
			return fmt.Errorf("%w: destination prefix %s already contains data", errStorageMigrationStateConflict, prefix.String())
		}
		if err := w.deps.DB.WithContext(ctx).Model(item).Update("destination_owned", true).Error; err != nil {
			return err
		}
		item.DestinationOwned = true
	}
	objects, err := storage.PrefixInventory(ctx, source, prefix)
	if err != nil {
		return err
	}
	if len(objects) == 0 {
		return fmt.Errorf("source prefix %s is empty", prefix.String())
	}
	var total int64
	for _, object := range objects {
		total += object.Size
	}
	status, message := models.StorageMigrationItemCopying, "Copying and verifying media"
	if finalSync {
		status, message = models.StorageMigrationItemVerifying, "Final synchronization and verification"
	}
	if err := w.deps.DB.WithContext(ctx).Model(item).Updates(map[string]any{
		"status": status, "bytes_total": total, "object_count": len(objects), "progress_message": message,
	}).Error; err != nil {
		return err
	}
	verifiedKeys := make(map[string]bool, len(objects))
	var verifiedBytes int64
	lastProgressUpdate := time.Time{}
	for index, object := range objects {
		if _, err := storage.CopyObjectVerified(ctx, source, destination, object); err != nil {
			return fmt.Errorf("copy %s: %w", object.Key.String(), err)
		}
		verifiedKeys[object.Key.String()] = true
		verifiedBytes += object.Size
		now := time.Now()
		if index+1 == len(objects) || lastProgressUpdate.IsZero() || now.Sub(lastProgressUpdate) >= 500*time.Millisecond {
			if err := w.deps.DB.WithContext(ctx).Model(item).Updates(map[string]any{
				"bytes_copied": verifiedBytes, "objects_verified": index + 1,
				"progress_message": fmt.Sprintf("Verified %d of %d objects", index+1, len(objects)),
			}).Error; err != nil {
				return err
			}
			if err := w.refreshStorageMigrationProgress(ctx, item.MigrationID); err != nil {
				return err
			}
			lastProgressUpdate = now
		}
	}
	if finalSync {
		destinationObjects, err := storage.PrefixInventory(ctx, destination, prefix)
		if err != nil {
			return err
		}
		for _, object := range destinationObjects {
			if !verifiedKeys[object.Key.String()] {
				if err := destination.Delete(ctx, object.Key); err != nil {
					return fmt.Errorf("remove stale destination object %s: %w", object.Key.String(), err)
				}
			}
		}
	}
	return nil
}

func (w *WorkerGroup) refreshStorageMigrationProgress(ctx context.Context, migrationID uint) error {
	var aggregate struct {
		ActualBytes  int64
		CopiedBytes  int64
		CutoverCount int64
		CleanedCount int64
		FileCount    int64
	}
	err := w.deps.DB.WithContext(ctx).Model(&models.StorageMigrationItem{}).
		Select(`COALESCE(SUM(CASE WHEN bytes_total > 0 THEN bytes_total ELSE planned_bytes END), 0) AS actual_bytes,
			COALESCE(SUM(bytes_copied), 0) AS copied_bytes,
			SUM(CASE WHEN status IN ? THEN 1 ELSE 0 END) AS cutover_count,
			SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS cleaned_count,
			COUNT(*) AS file_count`, []string{models.StorageMigrationItemCleanupPending, models.StorageMigrationItemCleaning, models.StorageMigrationItemCleaned, models.StorageMigrationItemOriginalKept, models.StorageMigrationItemOriginalPartial}, models.StorageMigrationItemCleaned).
		Where("migration_id = ?", migrationID).Scan(&aggregate).Error
	if err != nil {
		return err
	}
	if err := w.deps.DB.WithContext(ctx).Model(&models.StorageMigration{}).Where("id = ?", migrationID).Updates(map[string]any{
		"actual_bytes": aggregate.ActualBytes, "copied_bytes": aggregate.CopiedBytes,
		"cutover_count": aggregate.CutoverCount, "cleaned_count": aggregate.CleanedCount,
	}).Error; err != nil {
		return err
	}
	progress := 0.0
	if aggregate.ActualBytes > 0 {
		progress = float64(aggregate.CopiedBytes) / float64(aggregate.ActualBytes)
	}
	background.ReportProgress(ctx, progress, fmt.Sprintf("%d of %d videos active on destination", aggregate.CutoverCount, aggregate.FileCount))
	return nil
}

func (w *WorkerGroup) scheduleStorageMigrationCleanup(ctx context.Context, runtime *background.Runtime, migration *models.StorageMigration) error {
	if runtime == nil {
		return errors.New("background runtime is unavailable")
	}
	if err := w.deps.DB.WithContext(ctx).First(migration, migration.ID).Error; err != nil {
		return err
	}
	if migration.CleanupJobID != "" {
		return nil
	}
	cleanupAfter := time.Now().UTC().Add(storageMigrationCleanupGrace)
	if migration.CleanupAfter != nil {
		cleanupAfter = migration.CleanupAfter.UTC()
	}
	job, _, err := runtime.Enqueue(ctx, background.JobSpec{
		Kind: "storage.migration.cleanup", Visibility: background.VisibilityAdmin,
		SubjectType: "storage_migration", SubjectID: migration.UUID,
		IdempotencyKey: "storage-migration-cleanup:" + migration.UUID,
		Label:          fmt.Sprintf("Clean originals from %s", migration.SourcePoolName),
		Pausable:       true,
		Tasks: []background.TaskSpec{{
			Kind: taskStorageCleanup, Queue: background.QueueStorage, Phase: "Retaining originals",
			Payload: storageMigrationTaskPayload{MigrationID: migration.ID}, DedupeKey: fmt.Sprintf("storage-cleanup:%d", migration.ID),
			Priority: 5, Required: true, Weight: 1, MaxAttempts: 4, RunAfter: &cleanupAfter,
		}},
	})
	if err != nil {
		return err
	}
	copyCompletedAt := time.Now().UTC()
	if err := w.deps.DB.WithContext(ctx).Model(migration).Updates(map[string]any{
		"status": models.StorageMigrationRetainingOriginals, "phase": "Retaining originals for 24 hours",
		"cleanup_job_id": job.ID, "cleanup_after": &cleanupAfter, "copy_completed_at": &copyCompletedAt,
		"error_code": "", "error_message": "",
	}).Error; err != nil {
		return err
	}
	migration.CleanupJobID = job.ID
	return nil
}

func (w *WorkerGroup) storageMigrationCleanupHandler(ctx context.Context, task background.Task) (background.Result, error) {
	var payload storageMigrationTaskPayload
	if err := decodeTaskPayload(task, &payload); err != nil {
		return background.Result{}, err
	}
	var migration models.StorageMigration
	if err := w.deps.DB.WithContext(ctx).First(&migration, payload.MigrationID).Error; err != nil {
		return background.Result{}, background.Permanent("migration_missing", "The storage migration no longer exists", err)
	}
	if migration.KeepOriginals || migration.Status == models.StorageMigrationOriginalsRetained {
		return background.Result{ResultType: "storage_migration", ResultID: migration.UUID, Phase: "Originals retained"}, nil
	}
	if err := w.deps.DB.WithContext(ctx).Model(&migration).Updates(map[string]any{
		"status": models.StorageMigrationCleaningOriginals, "phase": "Cleaning original copies",
	}).Error; err != nil {
		return background.Result{}, background.Transient("cleanup_state_failed", "Cleanup state could not be updated", err)
	}
	var items []models.StorageMigrationItem
	if err := w.deps.DB.WithContext(ctx).Where("migration_id = ? AND status <> ?", migration.ID, models.StorageMigrationItemCleaned).Order("id ASC").Find(&items).Error; err != nil {
		return background.Result{}, background.Transient("cleanup_items_failed", "Cleanup videos could not be loaded", err)
	}
	for index := range items {
		if err := w.cleanupStorageMigrationItem(ctx, &items[index]); err != nil {
			if ctx.Err() != nil {
				if reloadErr := w.deps.DB.WithContext(context.WithoutCancel(ctx)).First(&migration, migration.ID).Error; reloadErr == nil && migration.KeepOriginals {
					return background.Result{}, ctx.Err()
				}
				status, phase := models.StorageMigrationCanceled, "Cleanup canceled; remaining originals retained"
				if background.PauseRequested(ctx) {
					status, phase = models.StorageMigrationPaused, "Cleanup paused"
				}
				_ = w.deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&migration).Updates(map[string]any{"status": status, "phase": phase}).Error
				return background.Result{}, ctx.Err()
			}
			return background.Result{}, background.Transient("cleanup_failed", "An original copy could not be removed", err)
		}
		if err := w.refreshStorageMigrationProgress(context.WithoutCancel(ctx), migration.ID); err != nil {
			return background.Result{}, background.Transient("cleanup_progress_failed", "Original cleanup progress could not be recorded", err)
		}
		background.ReportProgress(ctx, float64(index+1)/float64(len(items)), fmt.Sprintf("Cleaned %d of %d originals", index+1, len(items)))
		if err := ctx.Err(); err != nil {
			status, phase := models.StorageMigrationCanceled, "Cleanup canceled; remaining originals retained"
			if background.PauseRequested(ctx) {
				status, phase = models.StorageMigrationPaused, "Cleanup paused"
			}
			_ = w.deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&migration).Updates(map[string]any{"status": status, "phase": phase}).Error
			return background.Result{}, err
		}
	}
	if err := w.deps.DB.WithContext(context.WithoutCancel(ctx)).First(&migration, migration.ID).Error; err == nil && migration.KeepOriginals {
		return background.Result{ResultType: "storage_migration", ResultID: migration.UUID, Phase: "Originals retained"}, nil
	}
	completedAt := time.Now().UTC()
	if err := w.deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&migration).Updates(map[string]any{
		"status": models.StorageMigrationCompleted, "phase": "Migration and cleanup complete", "cleaned_count": migration.FileCount,
		"completed_at": &completedAt,
	}).Error; err != nil {
		return background.Result{}, background.Transient("cleanup_state_failed", "Cleanup completion could not be recorded", err)
	}
	return background.Result{ResultType: "storage_migration", ResultID: migration.UUID, Phase: "Cleanup complete"}, nil
}

func (w *WorkerGroup) cleanupStorageMigrationItem(ctx context.Context, item *models.StorageMigrationItem) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if item.Status == models.StorageMigrationItemCleaned || item.Status == models.StorageMigrationItemOriginalKept || item.Status == models.StorageMigrationItemOriginalPartial {
		return nil
	}
	releaseFile := w.deps.StorageLifecycle.FileWriteLock(item.FileID)
	defer releaseFile()
	releaseMount := w.deps.StorageLifecycle.ReadLock(item.SourceMountID)
	defer releaseMount()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := w.deps.DB.WithContext(ctx).First(item, item.ID).Error; err != nil {
		return err
	}
	if item.Status == models.StorageMigrationItemCleaned || item.Status == models.StorageMigrationItemOriginalKept || item.Status == models.StorageMigrationItemOriginalPartial {
		return nil
	}
	var file models.File
	err := w.deps.DB.WithContext(ctx).Unscoped().First(&file, item.FileID).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err == nil && file.StorageID != item.DestinationMountID {
		return fmt.Errorf("%w: file %d is no longer on expected destination", errStorageMigrationStateConflict, item.FileID)
	}
	store, err := w.deps.Storage.Store(item.SourceMountID)
	if err != nil {
		return err
	}
	prefix, err := w.deps.Storage.Layout().FilePrefix(item.FileUUID)
	if err != nil {
		return err
	}
	if err := w.deps.DB.WithContext(ctx).Model(item).Updates(map[string]any{
		"status": models.StorageMigrationItemCleaning, "progress_message": "Removing original copy",
	}).Error; err != nil {
		return err
	}
	// Once cleanup begins for a video, finish that whole video before honoring a
	// pause or cancel. This prevents a retained original from being only partly
	// deleted while still presenting cleanup as safely stopped.
	commitCtx := context.WithoutCancel(ctx)
	if err := storage.DeletePrefix(commitCtx, store, prefix); err != nil {
		return err
	}
	now := time.Now().UTC()
	return w.deps.DB.WithContext(commitCtx).Model(item).Updates(map[string]any{
		"status": models.StorageMigrationItemCleaned, "cleaned_at": &now, "reservation_key": "",
		"progress_message": "Original removed", "error_code": "", "error_message": "",
	}).Error
}

func (w *WorkerGroup) storageMigrationAbortHandler(runtime *background.Runtime) background.Handler {
	return func(ctx context.Context, task background.Task) (background.Result, error) {
		var payload storageMigrationTaskPayload
		if err := decodeTaskPayload(task, &payload); err != nil {
			return background.Result{}, err
		}
		var migration models.StorageMigration
		if err := w.deps.DB.WithContext(ctx).First(&migration, payload.MigrationID).Error; err != nil {
			return background.Result{}, background.Permanent("migration_missing", "The canceled migration no longer exists", err)
		}
		if migration.BackgroundJobID != "" && runtime != nil {
			if mainJob, err := runtime.Job(ctx, migration.BackgroundJobID, nil, true); err == nil && !storageMigrationJobTerminal(mainJob.Status) {
				return background.Result{}, &background.TaskError{Code: "migration_still_stopping", Public: "The migration is still stopping", Class: background.ErrorTransient, RetryAfter: 5 * time.Second}
			}
		}
		var items []models.StorageMigrationItem
		if err := w.deps.DB.WithContext(ctx).Where("migration_id = ?", migration.ID).Order("id ASC").Find(&items).Error; err != nil {
			return background.Result{}, background.Transient("abort_items_failed", "Canceled migration videos could not be loaded", err)
		}
		for index := range items {
			if err := w.cleanupCanceledStorageMigrationItem(ctx, &items[index]); err != nil {
				if ctx.Err() != nil {
					return background.Result{}, ctx.Err()
				}
				return background.Result{}, background.Transient("abort_cleanup_failed", "Incomplete destination data could not be cleaned", err)
			}
			background.ReportProgress(ctx, float64(index+1)/float64(len(items)), fmt.Sprintf("Reconciled %d of %d videos", index+1, len(items)))
			if err := ctx.Err(); err != nil {
				phase := "Canceled; incomplete destination cleanup stopped"
				if background.PauseRequested(ctx) {
					phase = "Canceled; incomplete destination cleanup paused"
				}
				_ = w.deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&migration).Update("phase", phase).Error
				return background.Result{}, err
			}
		}
		if err := w.refreshStorageMigrationProgress(context.WithoutCancel(ctx), migration.ID); err != nil {
			return background.Result{}, background.Transient("abort_progress_failed", "Canceled migration progress could not be reconciled", err)
		}
		if err := w.deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&migration).Updates(map[string]any{
			"status": models.StorageMigrationCanceled, "phase": "Canceled; incomplete destination data cleaned and originals retained",
			"error_code": "", "error_message": "",
		}).Error; err != nil {
			return background.Result{}, background.Transient("abort_state_failed", "Canceled migration state could not be finalized", err)
		}
		return background.Result{ResultType: "storage_migration", ResultID: migration.UUID, Phase: "Canceled migration cleaned"}, nil
	}
}

func (w *WorkerGroup) cleanupCanceledStorageMigrationItem(ctx context.Context, item *models.StorageMigrationItem) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	releaseFile := w.deps.StorageLifecycle.FileWriteLock(item.FileID)
	defer releaseFile()
	releaseMount := w.deps.StorageLifecycle.ReadLock(item.DestinationMountID)
	defer releaseMount()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := w.deps.DB.WithContext(ctx).First(item, item.ID).Error; err != nil {
		return err
	}
	var file models.File
	err := w.deps.DB.WithContext(ctx).First(&file, item.FileID).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err == nil && file.StorageID == item.DestinationMountID && item.CutoverAt != nil {
		return w.deps.DB.WithContext(context.WithoutCancel(ctx)).Model(item).Updates(map[string]any{
			"status": models.StorageMigrationItemOriginalKept, "reservation_key": "",
			"progress_message": "Destination remains active; original retained",
		}).Error
	}
	if err == nil && file.StorageID != item.SourceMountID {
		return fmt.Errorf("%w: file %d moved to unexpected mount %s", errStorageMigrationStateConflict, file.ID, file.StorageID)
	}
	if item.DestinationOwned {
		store, err := w.deps.Storage.Store(item.DestinationMountID)
		if err != nil {
			return err
		}
		prefix, err := w.deps.Storage.Layout().FilePrefix(item.FileUUID)
		if err != nil {
			return err
		}
		if err := storage.DeletePrefix(context.WithoutCancel(ctx), store, prefix); err != nil {
			return err
		}
	}
	return w.deps.DB.WithContext(context.WithoutCancel(ctx)).Model(item).Updates(map[string]any{
		"status": models.StorageMigrationItemCanceled, "reservation_key": "", "destination_owned": false,
		"bytes_copied": 0, "objects_verified": 0, "progress_message": "Canceled before cutover",
	}).Error
}

func storageMigrationJobTerminal(status string) bool {
	switch status {
	case background.JobSucceeded, background.JobSucceededWithWarnings, background.JobFailed, background.JobCanceled:
		return true
	default:
		return false
	}
}

func (w *WorkerGroup) storageMigrationReconcileHandler(runtime *background.Runtime) background.Handler {
	return func(ctx context.Context, task background.Task) (background.Result, error) {
		if runtime == nil || w.logic == nil {
			return background.Result{}, background.Permanent("migration_runtime_missing", "Storage migration reconciliation is unavailable", errors.New("storage migration runtime is unavailable"))
		}
		var migrations []models.StorageMigration
		if err := w.deps.DB.WithContext(ctx).
			Where("status IN ?", []string{
				models.StorageMigrationQueued, models.StorageMigrationRunning, models.StorageMigrationPaused,
				models.StorageMigrationCanceled, models.StorageMigrationRetainingOriginals, models.StorageMigrationCleaningOriginals,
			}).
			Order("id ASC").Find(&migrations).Error; err != nil {
			return background.Result{}, background.Transient("migration_reconcile_load_failed", "Storage migrations could not be inspected", err)
		}
		reconciled := 0
		for index := range migrations {
			if err := ctx.Err(); err != nil {
				return background.Result{}, err
			}
			migration := &migrations[index]
			durableCtx := context.WithoutCancel(ctx)
			if migration.CleanupJobID != "" {
				cleanupJob, err := runtime.Job(ctx, migration.CleanupJobID, nil, true)
				if errors.Is(err, gorm.ErrRecordNotFound) {
					if migration.Status == models.StorageMigrationCanceled {
						if err := w.logic.EnsureStorageMigrationAbortCleanup(durableCtx, migration.ID); err != nil {
							return background.Result{}, background.Transient("migration_reconcile_abort_failed", "Canceled migration cleanup could not be restored", err)
						}
					} else {
						wasPaused := migration.Status == models.StorageMigrationPaused
						if err := w.deps.DB.WithContext(durableCtx).Model(migration).Update("cleanup_job_id", "").Error; err != nil {
							return background.Result{}, background.Transient("migration_reconcile_link_failed", "Original cleanup could not be relinked", err)
						}
						migration.CleanupJobID = ""
						if err := w.scheduleStorageMigrationCleanup(durableCtx, runtime, migration); err != nil {
							return background.Result{}, background.Transient("migration_reconcile_cleanup_failed", "Original cleanup could not be restored", err)
						}
						if wasPaused {
							if err := runtime.PauseJob(durableCtx, migration.CleanupJobID, 0, "VideoCMS"); err != nil && !errors.Is(err, background.ErrConflict) {
								return background.Result{}, background.Transient("migration_reconcile_pause_failed", "Paused original cleanup could not be restored", err)
							}
							if err := w.deps.DB.WithContext(durableCtx).Model(migration).Updates(map[string]any{
								"status": models.StorageMigrationPaused, "phase": "Original cleanup paused",
							}).Error; err != nil {
								return background.Result{}, background.Transient("migration_reconcile_state_failed", "Paused cleanup status could not be restored", err)
							}
						}
					}
					reconciled++
				} else if err != nil {
					return background.Result{}, background.Transient("migration_reconcile_job_failed", "Original cleanup status could not be inspected", err)
				} else if cleanupJob.Kind == "storage.migration.cleanup" {
					switch cleanupJob.Status {
					case background.JobCanceled:
						if _, err := w.logic.KeepStorageMigrationOriginals(durableCtx, migration.UUID, 0, "VideoCMS"); err != nil {
							return background.Result{}, background.Transient("migration_reconcile_retention_failed", "Canceled original cleanup could not be reconciled", err)
						}
						reconciled++
					case background.JobPauseRequested, background.JobPaused:
						if migration.Status != models.StorageMigrationPaused {
							if err := w.deps.DB.WithContext(durableCtx).Model(migration).Updates(map[string]any{"status": models.StorageMigrationPaused, "phase": "Original cleanup paused"}).Error; err != nil {
								return background.Result{}, background.Transient("migration_reconcile_state_failed", "Paused cleanup status could not be repaired", err)
							}
							reconciled++
						}
					case background.JobQueued, background.JobRetryWait, background.JobRunning:
						status, phase := models.StorageMigrationCleaningOriginals, "Cleaning original copies"
						if cleanupJob.Status != background.JobRunning && migration.CleanupAfter != nil && migration.CleanupAfter.After(time.Now().UTC()) {
							status, phase = models.StorageMigrationRetainingOriginals, "Retaining originals until cleanup begins"
						}
						if migration.Status != status || migration.Phase != phase {
							if err := w.deps.DB.WithContext(durableCtx).Model(migration).Updates(map[string]any{"status": status, "phase": phase}).Error; err != nil {
								return background.Result{}, background.Transient("migration_reconcile_state_failed", "Cleanup status could not be repaired", err)
							}
							reconciled++
						}
					case background.JobSucceeded, background.JobSucceededWithWarnings:
						completedAt := time.Now().UTC()
						if err := w.refreshStorageMigrationProgress(durableCtx, migration.ID); err != nil {
							return background.Result{}, background.Transient("migration_reconcile_progress_failed", "Cleanup progress could not be repaired", err)
						}
						if err := w.deps.DB.WithContext(durableCtx).Model(migration).Updates(map[string]any{
							"status": models.StorageMigrationCompleted, "phase": "Migration and cleanup complete", "completed_at": &completedAt,
						}).Error; err != nil {
							return background.Result{}, background.Transient("migration_reconcile_state_failed", "Completed cleanup status could not be repaired", err)
						}
						reconciled++
					case background.JobFailed:
						if migration.Phase != "Original cleanup needs attention" {
							if err := w.deps.DB.WithContext(durableCtx).Model(migration).Updates(map[string]any{
								"phase": "Original cleanup needs attention", "error_code": cleanupJob.ErrorCode, "error_message": cleanupJob.ErrorMessage,
							}).Error; err != nil {
								return background.Result{}, background.Transient("migration_reconcile_state_failed", "Failed cleanup status could not be repaired", err)
							}
							reconciled++
						}
					}
				} else if cleanupJob.Kind == "storage.migration.abort_cleanup" {
					phase := "Canceled; cleaning incomplete destination data"
					if cleanupJob.Status == background.JobPauseRequested || cleanupJob.Status == background.JobPaused {
						phase = "Canceled; incomplete destination cleanup paused"
					} else if cleanupJob.Status == background.JobCanceled {
						phase = "Canceled; incomplete destination cleanup stopped"
					} else if cleanupJob.Status == background.JobFailed {
						phase = "Canceled; incomplete destination cleanup needs attention"
					} else if cleanupJob.Status == background.JobSucceeded || cleanupJob.Status == background.JobSucceededWithWarnings {
						phase = "Canceled; incomplete destination data cleaned and originals retained"
					}
					if migration.Status != models.StorageMigrationCanceled || migration.Phase != phase {
						if err := w.deps.DB.WithContext(durableCtx).Model(migration).Updates(map[string]any{"status": models.StorageMigrationCanceled, "phase": phase}).Error; err != nil {
							return background.Result{}, background.Transient("migration_reconcile_state_failed", "Canceled migration status could not be repaired", err)
						}
						reconciled++
					}
				}
				background.ReportProgress(ctx, float64(index+1)/float64(len(migrations)), fmt.Sprintf("Inspected %d of %d migrations", index+1, len(migrations)))
				continue
			}

			if migration.Status == models.StorageMigrationCanceled {
				if err := w.logic.EnsureStorageMigrationAbortCleanup(durableCtx, migration.ID); err != nil {
					return background.Result{}, background.Transient("migration_reconcile_abort_failed", "A canceled migration could not be reconciled", err)
				}
				reconciled++
				background.ReportProgress(ctx, float64(index+1)/float64(len(migrations)), fmt.Sprintf("Inspected %d of %d migrations", index+1, len(migrations)))
				continue
			}
			if migration.Status == models.StorageMigrationRetainingOriginals || migration.Status == models.StorageMigrationCleaningOriginals {
				if err := w.scheduleStorageMigrationCleanup(durableCtx, runtime, migration); err != nil {
					return background.Result{}, background.Transient("migration_reconcile_cleanup_failed", "Original cleanup could not be restored", err)
				}
				reconciled++
				background.ReportProgress(ctx, float64(index+1)/float64(len(migrations)), fmt.Sprintf("Inspected %d of %d migrations", index+1, len(migrations)))
				continue
			}

			mainMissing := false
			var persistedMain background.Job
			mainQuery := w.deps.DB.WithContext(ctx)
			if migration.BackgroundJobID != "" {
				mainQuery = mainQuery.Where("id = ?", migration.BackgroundJobID)
			} else {
				mainQuery = mainQuery.Where("kind = ? AND subject_type = ? AND subject_id = ?", "storage.migration", "storage_migration", migration.UUID).Order("created_at DESC")
			}
			if err := mainQuery.First(&persistedMain).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				mainMissing = true
			} else if err != nil {
				return background.Result{}, background.Transient("migration_reconcile_main_failed", "The migration job could not be inspected", err)
			}
			mainJob, err := w.logic.EnsureStorageMigrationJob(durableCtx, migration.ID)
			if err != nil {
				return background.Result{}, background.Transient("migration_reconcile_main_failed", "The migration job could not be restored", err)
			}
			if migration.BackgroundJobID != mainJob.ID {
				migration.BackgroundJobID = mainJob.ID
				reconciled++
			}
			if mainMissing && migration.Status == models.StorageMigrationPaused && mainJob.Status != background.JobPaused && mainJob.Status != background.JobPauseRequested {
				if err := runtime.PauseJob(durableCtx, mainJob.ID, 0, "VideoCMS"); err != nil && !errors.Is(err, background.ErrConflict) {
					return background.Result{}, background.Transient("migration_reconcile_pause_failed", "Paused migration status could not be restored", err)
				}
				mainJob, err = w.logic.EnsureStorageMigrationJob(durableCtx, migration.ID)
				if err != nil {
					return background.Result{}, background.Transient("migration_reconcile_main_failed", "The paused migration job could not be reloaded", err)
				}
			}
			switch mainJob.Status {
			case background.JobCanceled:
				if err := w.logic.EnsureStorageMigrationAbortCleanup(durableCtx, migration.ID); err != nil {
					return background.Result{}, background.Transient("migration_reconcile_abort_failed", "A canceled migration could not be reconciled", err)
				}
				reconciled++
			case background.JobPauseRequested, background.JobPaused:
				if migration.Status != models.StorageMigrationPaused {
					if err := w.deps.DB.WithContext(durableCtx).Model(migration).Updates(map[string]any{"status": models.StorageMigrationPaused, "phase": "Migration paused"}).Error; err != nil {
						return background.Result{}, background.Transient("migration_reconcile_state_failed", "Paused migration status could not be repaired", err)
					}
					reconciled++
				}
			case background.JobQueued, background.JobRetryWait:
				if migration.Status != models.StorageMigrationQueued {
					if err := w.deps.DB.WithContext(durableCtx).Model(migration).Updates(map[string]any{"status": models.StorageMigrationQueued, "phase": "Waiting to resume migration"}).Error; err != nil {
						return background.Result{}, background.Transient("migration_reconcile_state_failed", "Queued migration status could not be repaired", err)
					}
					reconciled++
				}
			case background.JobRunning:
				if migration.Status != models.StorageMigrationRunning {
					if err := w.deps.DB.WithContext(durableCtx).Model(migration).Updates(map[string]any{"status": models.StorageMigrationRunning, "phase": "Migrating videos"}).Error; err != nil {
						return background.Result{}, background.Transient("migration_reconcile_state_failed", "Running migration status could not be repaired", err)
					}
					reconciled++
				}
			case background.JobSucceeded, background.JobSucceededWithWarnings:
				if err := w.scheduleStorageMigrationCleanup(durableCtx, runtime, migration); err != nil {
					return background.Result{}, background.Transient("migration_reconcile_cleanup_failed", "Original cleanup could not be restored", err)
				}
				reconciled++
			case background.JobFailed:
				if migration.Status != models.StorageMigrationFailed {
					if err := w.deps.DB.WithContext(durableCtx).Model(migration).Updates(map[string]any{
						"status": models.StorageMigrationFailed, "phase": "Migration needs attention",
						"error_code": mainJob.ErrorCode, "error_message": mainJob.ErrorMessage,
					}).Error; err != nil {
						return background.Result{}, background.Transient("migration_reconcile_state_failed", "Failed migration status could not be repaired", err)
					}
					reconciled++
				}
			}
			background.ReportProgress(ctx, float64(index+1)/float64(len(migrations)), fmt.Sprintf("Inspected %d of %d migrations", index+1, len(migrations)))
		}
		return background.Result{Value: map[string]any{"reconciled": reconciled}, Phase: "Storage migrations reconciled"}, nil
	}
}
