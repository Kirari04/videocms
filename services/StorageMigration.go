package services

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"ch/kirari04/videocms/background"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	storageMigrationCleanupGrace        = 24 * time.Hour
	storageMigrationEncodingRetry       = 5 * time.Minute
	storageMigrationObjectConcurrency   = 6
	storageMigrationObjectMaxAttempts   = 5
	storageMigrationCheckpointBatchSize = 64
)

var errStorageMigrationStateConflict = errors.New("storage migration state changed")
var errStorageMigrationItemDeleted = errors.New("storage migration video was deleted")
var errStorageMigrationEncodingActive = errors.New("video encoding is still active")

type storageMigrationTaskPayload struct {
	MigrationID uint `json:"migrationId"`
}

type storageMigrationObjectResult struct {
	source      storage.ObjectInfo
	destination storage.ObjectInfo
	checkpoint  *models.StorageMigrationObject
	err         error
}

type storageMigrationCopyProgress struct {
	totalObjects    int
	verifiedObjects int
	verifiedBytes   int64
	verifiedKeys    map[string]storage.ObjectInfo
	pending         []models.StorageMigrationObject
	lastPersistedAt time.Time
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
			Where("migration_id = ? AND status NOT IN ?", migration.ID, []string{models.StorageMigrationItemCleanupPending, models.StorageMigrationItemCleaned, models.StorageMigrationItemDeleted}).
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
			if item.Status == models.StorageMigrationItemDeleted {
				continue
			}
			var file models.File
			if err := tx.Unscoped().First(&file, item.FileID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					if err := markStorageMigrationItemDeleted(tx, item); err != nil {
						return err
					}
					continue
				}
				return err
			}
			if file.DeletedAt != nil && file.DeletedAt.Valid {
				if err := markStorageMigrationItemDeleted(tx, item); err != nil {
					return err
				}
				continue
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
			updated := tx.Model(item).Where("status <> ?", models.StorageMigrationItemDeleted).Updates(updates)
			if updated.Error != nil {
				if strings.Contains(strings.ToLower(updated.Error.Error()), "unique") {
					return errStorageMigrationStateConflict
				}
				return updated.Error
			}
			if updated.RowsAffected == 0 {
				continue
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
	if item.Status == models.StorageMigrationItemCleanupPending || item.Status == models.StorageMigrationItemCleaned || item.Status == models.StorageMigrationItemDeleted {
		return nil
	}
	defer func() {
		if err == nil {
			return
		}
		status, code, message := models.StorageMigrationItemFailed, "copy_failed", boundedServiceError(err.Error())
		if ctx.Err() != nil {
			status, code, message = models.StorageMigrationItemPending, "", "Paused at a safe checkpoint"
		}
		_ = w.deps.DB.WithContext(context.WithoutCancel(ctx)).Model(item).
			Where("status <> ?", models.StorageMigrationItemDeleted).Updates(map[string]any{
			"status": status, "error_code": code, "error_message": message, "progress_message": message,
		}).Error
	}()

	releaseFile := w.deps.StorageLifecycle.FileReadLock(item.FileID)
	if err = w.deps.DB.WithContext(ctx).First(item, item.ID).Error; err != nil {
		releaseFile()
		return err
	}
	if item.Status == models.StorageMigrationItemDeleted {
		releaseFile()
		return nil
	}
	var file models.File
	if err = w.deps.DB.WithContext(ctx).First(&file, item.FileID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = markStorageMigrationItemDeleted(w.deps.DB.WithContext(context.WithoutCancel(ctx)), item)
			if err == nil {
				releaseMount := w.deps.StorageLifecycle.ReadLock(item.DestinationMountID)
				err = w.cleanupDeletedStorageMigrationDestination(context.WithoutCancel(ctx), item, false)
				releaseMount()
			}
		}
		releaseFile()
		return err
	}
	now := time.Now().UTC()
	if err = updateActiveStorageMigrationItem(w.deps.DB.WithContext(ctx), item, map[string]any{
		"status": models.StorageMigrationItemCopying, "copy_started_at": gorm.Expr("COALESCE(copy_started_at, ?)", now),
		"error_code": "", "error_message": "", "progress_message": "Copying and verifying media",
	}); err != nil {
		releaseFile()
		if errors.Is(err, errStorageMigrationItemDeleted) {
			return nil
		}
		return err
	}
	if file.StorageID != item.SourceMountID || file.StorageState != models.FileStorageAvailable {
		releaseFile()
		return fmt.Errorf("%w: file %d is no longer on source mount %s", errStorageMigrationStateConflict, file.ID, item.SourceMountID)
	}
	releaseMounts := w.deps.StorageLifecycle.ReadLocks(item.SourceMountID, item.DestinationMountID)
	if err = w.copyStorageMigrationPrefix(ctx, item, file, false); err != nil {
		if errors.Is(err, errStorageMigrationItemDeleted) {
			err = w.cleanupDeletedStorageMigrationDestination(context.WithoutCancel(ctx), item, true)
		}
		releaseMounts()
		releaseFile()
		if errors.Is(err, errStorageMigrationItemDeleted) {
			return nil
		}
		return err
	}
	releaseMounts()
	releaseFile()

	if err = ctx.Err(); err != nil {
		return err
	}
	releaseFile = w.deps.StorageLifecycle.FileWriteLock(item.FileID)
	defer releaseFile()
	if err = w.deps.DB.WithContext(ctx).First(item, item.ID).Error; err != nil {
		return err
	}
	if item.Status == models.StorageMigrationItemDeleted {
		releaseMount := w.deps.StorageLifecycle.ReadLock(item.DestinationMountID)
		defer releaseMount()
		return w.cleanupDeletedStorageMigrationDestination(context.WithoutCancel(ctx), item, true)
	}
	releaseMounts = w.deps.StorageLifecycle.ReadLocks(item.SourceMountID, item.DestinationMountID)
	defer releaseMounts()
	if err = w.deps.DB.WithContext(ctx).First(&file, item.FileID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err = markStorageMigrationItemDeleted(w.deps.DB.WithContext(context.WithoutCancel(ctx)), item); err == nil {
				err = w.cleanupDeletedStorageMigrationDestination(context.WithoutCancel(ctx), item, true)
			}
		}
		return err
	}
	if file.StorageID != item.SourceMountID || file.StorageState != models.FileStorageAvailable {
		return fmt.Errorf("%w: file %d changed before cutover", errStorageMigrationStateConflict, file.ID)
	}
	if err = w.copyStorageMigrationPrefix(ctx, item, file, true); err != nil {
		if errors.Is(err, errStorageMigrationItemDeleted) {
			if cleanupErr := w.cleanupDeletedStorageMigrationDestination(context.WithoutCancel(ctx), item, true); cleanupErr != nil {
				return cleanupErr
			}
			return nil
		}
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	cutoverAt := time.Now().UTC()
	if w.deps.MediaCache != nil {
		if err = w.deps.MediaCache.InvalidateFile(context.WithoutCancel(ctx), file.ID); err != nil {
			return fmt.Errorf("invalidate playback cache before cutover: %w", err)
		}
	}
	err = w.deps.DB.WithContext(context.WithoutCancel(ctx)).Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&models.File{}).
			Where("id = ? AND storage_id = ? AND storage_state = ?", file.ID, item.SourceMountID, models.FileStorageAvailable).
			Updates(map[string]any{
				"storage_id": item.DestinationMountID, "storage_pool_id": migration.DestinationPoolID,
				"storage_cache_version": gorm.Expr("COALESCE(storage_cache_version, 0) + 1"),
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errStorageMigrationStateConflict
		}
		updated = tx.Model(item).Where("status <> ?", models.StorageMigrationItemDeleted).Updates(map[string]any{
			"status": models.StorageMigrationItemCleanupPending, "verified_at": &cutoverAt, "cutover_at": &cutoverAt,
			"progress_message": "Destination active; original retained", "error_code": "", "error_message": "",
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errStorageMigrationItemDeleted
		}
		if err := tx.Where("item_id = ?", item.ID).Delete(&models.StorageMigrationObject{}).Error; err != nil {
			return err
		}
		return nil
	})
	if errors.Is(err, errStorageMigrationItemDeleted) {
		if cleanupErr := w.cleanupDeletedStorageMigrationDestination(context.WithoutCancel(ctx), item, true); cleanupErr != nil {
			return cleanupErr
		}
		return nil
	}
	if errors.Is(err, errStorageMigrationStateConflict) {
		var current models.File
		loadErr := w.deps.DB.WithContext(context.WithoutCancel(ctx)).Unscoped().First(&current, file.ID).Error
		if errors.Is(loadErr, gorm.ErrRecordNotFound) || (loadErr == nil && current.DeletedAt != nil && current.DeletedAt.Valid) {
			if markErr := markStorageMigrationItemDeleted(w.deps.DB.WithContext(context.WithoutCancel(ctx)), item); markErr != nil {
				return markErr
			}
			if cleanupErr := w.cleanupDeletedStorageMigrationDestination(context.WithoutCancel(ctx), item, true); cleanupErr != nil {
				return cleanupErr
			}
			return nil
		}
		if loadErr != nil {
			return loadErr
		}
	}
	return err
}

func updateActiveStorageMigrationItem(db *gorm.DB, item *models.StorageMigrationItem, updates map[string]any) error {
	result := db.Model(item).Where("status <> ?", models.StorageMigrationItemDeleted).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var current models.StorageMigrationItem
		if err := db.First(&current, item.ID).Error; err != nil {
			return err
		}
		if current.Status == models.StorageMigrationItemDeleted {
			*item = current
			return errStorageMigrationItemDeleted
		}
	}
	return nil
}

func markStorageMigrationItemDeleted(db *gorm.DB, item *models.StorageMigrationItem) error {
	updates := map[string]any{
		"status": models.StorageMigrationItemDeleted, "error_code": "", "error_message": "",
		"progress_message": "Deleted during migration", "bytes_total": 0, "bytes_copied": 0,
		"objects_verified": 0,
	}
	if err := db.Model(item).Updates(updates).Error; err != nil {
		return err
	}
	item.Status = models.StorageMigrationItemDeleted
	item.ErrorCode = ""
	item.ErrorMessage = ""
	item.ProgressMessage = "Deleted during migration"
	item.BytesTotal = 0
	item.BytesCopied = 0
	item.ObjectsVerified = 0
	return nil
}

// cleanupDeletedStorageMigrationDestination removes any partial copy written
// by a migration worker that overlapped deletion in another process. The file
// deleter remains responsible for the source and releases the reservation only
// after every application-owned copy has been removed.
func (w *WorkerGroup) cleanupDeletedStorageMigrationDestination(ctx context.Context, item *models.StorageMigrationItem, workerWroteDestination bool) error {
	ownedDestination := item.DestinationOwned || workerWroteDestination
	if err := w.deps.DB.WithContext(ctx).First(item, item.ID).Error; err != nil {
		return err
	}
	if item.Status != models.StorageMigrationItemDeleted || (!item.DestinationOwned && !ownedDestination) {
		return nil
	}
	store, err := w.deps.Storage.Store(item.DestinationMountID)
	if err != nil {
		return err
	}
	prefix, err := w.deps.Storage.Layout().FilePrefix(item.FileUUID)
	if err != nil {
		return err
	}
	if err := storage.DeletePrefix(ctx, store, prefix); err != nil {
		return err
	}
	return w.deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("item_id = ?", item.ID).Delete(&models.StorageMigrationObject{}).Error; err != nil {
			return err
		}
		return tx.Model(item).Where("status = ?", models.StorageMigrationItemDeleted).Update("destination_owned", false).Error
	})
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
	destinationInventory, err := storage.PrefixInventory(ctx, destination, prefix)
	if err != nil {
		return err
	}
	if !item.DestinationOwned {
		if len(destinationInventory) > 0 {
			return fmt.Errorf("%w: destination prefix %s already contains data", errStorageMigrationStateConflict, prefix.String())
		}
		if err := w.clearStorageMigrationObjectCheckpoints(ctx, item.ID); err != nil {
			return err
		}
		if err := updateActiveStorageMigrationItem(w.deps.DB.WithContext(ctx), item, map[string]any{"destination_owned": true}); err != nil {
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
	if err := updateActiveStorageMigrationItem(w.deps.DB.WithContext(ctx), item, map[string]any{
		"status": status, "bytes_total": total, "object_count": len(objects), "progress_message": message,
	}); err != nil {
		return err
	}
	checkpoints, err := w.loadStorageMigrationObjectCheckpoints(ctx, item.ID)
	if err != nil {
		return err
	}
	destinationByKey := make(map[string]storage.ObjectInfo, len(destinationInventory))
	for _, object := range destinationInventory {
		destinationByKey[object.Key.String()] = object
	}
	regular := make([]storage.ObjectInfo, 0, len(objects))
	manifests := make([]storage.ObjectInfo, 0)
	for _, object := range objects {
		if strings.HasSuffix(strings.ToLower(object.Key.String()), ".m3u8") {
			manifests = append(manifests, object)
		} else {
			regular = append(regular, object)
		}
	}
	progress := &storageMigrationCopyProgress{
		totalObjects: len(objects), verifiedKeys: make(map[string]storage.ObjectInfo, len(objects)),
		pending: make([]models.StorageMigrationObject, 0, storageMigrationCheckpointBatchSize),
	}
	for _, batch := range [][]storage.ObjectInfo{regular, manifests} {
		if err := w.copyStorageMigrationObjectBatch(ctx, item, source, destination, batch, destinationByKey, checkpoints, progress); err != nil {
			return err
		}
	}
	if finalSync {
		destinationObjects, err := storage.PrefixInventory(ctx, destination, prefix)
		if err != nil {
			return err
		}
		finalDestination := make(map[string]storage.ObjectInfo, len(destinationObjects))
		for _, object := range destinationObjects {
			if _, verified := progress.verifiedKeys[object.Key.String()]; !verified {
				if err := destination.Delete(ctx, object.Key); err != nil {
					return fmt.Errorf("remove stale destination object %s: %w", object.Key.String(), err)
				}
				continue
			}
			finalDestination[object.Key.String()] = object
		}
		for key, expected := range progress.verifiedKeys {
			object, exists := finalDestination[key]
			if !exists || object.Size != expected.Size || (expected.ETag != "" && object.ETag != "" && expected.ETag != object.ETag) {
				return fmt.Errorf("final destination inventory mismatch for %s: stored size/etag %d/%q, expected %d/%q", key, object.Size, object.ETag, expected.Size, expected.ETag)
			}
		}
	}
	return nil
}

func (w *WorkerGroup) copyStorageMigrationObjectBatch(
	ctx context.Context,
	item *models.StorageMigrationItem,
	source storage.Store,
	destination storage.Store,
	objects []storage.ObjectInfo,
	destinationInventory map[string]storage.ObjectInfo,
	checkpoints map[string]models.StorageMigrationObject,
	progress *storageMigrationCopyProgress,
) error {
	if len(objects) == 0 {
		return nil
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan storage.ObjectInfo, len(objects))
	results := make(chan storageMigrationObjectResult, len(objects))
	for _, object := range objects {
		jobs <- object
	}
	close(jobs)
	workerCount := min(storageMigrationObjectConcurrency, len(objects))
	var workers sync.WaitGroup
	for index := 0; index < workerCount; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for object := range jobs {
				if workCtx.Err() != nil {
					return
				}
				checkpoint := checkpoints[object.Key.String()]
				destinationInfo, destinationExists := destinationInventory[object.Key.String()]
				result := copyStorageMigrationObject(workCtx, source, destination, object, destinationInfo, destinationExists, checkpoint)
				results <- result
				if result.err != nil {
					return
				}
			}
		}()
	}
	go func() {
		workers.Wait()
		close(results)
	}()

	var firstErr error
	for result := range results {
		if result.err != nil {
			if firstErr == nil && !errors.Is(result.err, context.Canceled) {
				firstErr = result.err
			}
			cancel()
			continue
		}
		progress.verifiedObjects++
		progress.verifiedBytes += result.source.Size
		verifiedDestination := result.destination
		verifiedDestination.Size = result.source.Size
		progress.verifiedKeys[result.source.Key.String()] = verifiedDestination
		if result.checkpoint != nil {
			progress.pending = append(progress.pending, *result.checkpoint)
		}
		now := time.Now()
		if len(progress.pending) >= storageMigrationCheckpointBatchSize || progress.lastPersistedAt.IsZero() || now.Sub(progress.lastPersistedAt) >= 500*time.Millisecond || progress.verifiedObjects == progress.totalObjects {
			if err := w.persistStorageMigrationCopyProgress(ctx, item, progress); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				cancel()
			}
		}
	}
	if len(progress.pending) > 0 {
		if err := w.persistStorageMigrationCopyProgress(context.WithoutCancel(ctx), item, progress); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil && ctx.Err() != nil {
		firstErr = ctx.Err()
	}
	return firstErr
}

func copyStorageMigrationObject(
	ctx context.Context,
	source storage.Store,
	destination storage.Store,
	object storage.ObjectInfo,
	destinationInfo storage.ObjectInfo,
	destinationExists bool,
	checkpoint models.StorageMigrationObject,
) storageMigrationObjectResult {
	revision := storageMigrationObjectRevision(object)
	if storageMigrationCheckpointMatches(ctx, destination, object, destinationInfo, destinationExists, checkpoint, revision) {
		return storageMigrationObjectResult{source: object, destination: destinationInfo}
	}
	var lastErr error
	for attempt := 1; attempt <= storageMigrationObjectMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return storageMigrationObjectResult{err: err}
		}
		current, err := source.Stat(ctx, object.Key)
		if err == nil {
			copied, copyErr := storage.CopyObjectValidated(ctx, source, destination, current)
			if copyErr == nil {
				after, statErr := source.Stat(ctx, object.Key)
				if statErr == nil && storageMigrationObjectRevision(after) == storageMigrationObjectRevision(copied.SourceInfo) {
					checkpoint := &models.StorageMigrationObject{
						ObjectKey: object.Key.String(), SourceRevision: storageMigrationObjectRevision(after),
						SourceSize: after.Size, DestinationETag: copied.Info.ETag,
						Checksum: copied.Checksum, UpdatedAt: time.Now().UTC(),
					}
					return storageMigrationObjectResult{source: after, destination: copied.Info, checkpoint: checkpoint}
				}
				if statErr != nil {
					copyErr = statErr
				} else {
					copyErr = errors.New("source object changed while it was being copied")
				}
			}
			err = copyErr
		}
		lastErr = err
		if attempt < storageMigrationObjectMaxAttempts {
			delay := time.Duration(1<<(attempt-1)) * 200 * time.Millisecond
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return storageMigrationObjectResult{err: ctx.Err()}
			case <-timer.C:
			}
		}
	}
	return storageMigrationObjectResult{err: fmt.Errorf("copy %s failed after %d attempts: %w", object.Key.String(), storageMigrationObjectMaxAttempts, lastErr)}
}

func storageMigrationCheckpointMatches(
	ctx context.Context,
	destination storage.Store,
	object storage.ObjectInfo,
	destinationInfo storage.ObjectInfo,
	destinationExists bool,
	checkpoint models.StorageMigrationObject,
	revision string,
) bool {
	if !destinationExists || destinationInfo.Size != object.Size || checkpoint.ObjectKey != object.Key.String() || checkpoint.SourceSize != object.Size || checkpoint.SourceRevision != revision {
		return false
	}
	if checkpoint.DestinationETag != "" && destinationInfo.ETag != "" {
		return checkpoint.DestinationETag == destinationInfo.ETag
	}
	if checkpoint.Checksum == "" {
		return false
	}

	// Local and SFTP stores have no stable ETag. On a resumed migration, hash
	// their existing destination once before trusting the durable checkpoint.
	// S3 stays on the cheap ETag path and does not download the object again.
	stored, err := destination.Open(ctx, object.Key)
	if err != nil {
		return false
	}
	digest := sha256.New()
	written, readErr := io.Copy(digest, stored.Body)
	closeErr := stored.Body.Close()
	return readErr == nil && closeErr == nil && written == object.Size && fmt.Sprintf("%x", digest.Sum(nil)) == checkpoint.Checksum
}

func storageMigrationObjectRevision(object storage.ObjectInfo) string {
	value := fmt.Sprintf("%s\x00%d\x00%d\x00%s", object.Key.String(), object.Size, object.ModTime.UTC().UnixNano(), object.ETag)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

func (w *WorkerGroup) loadStorageMigrationObjectCheckpoints(ctx context.Context, itemID uint) (map[string]models.StorageMigrationObject, error) {
	var rows []models.StorageMigrationObject
	if err := w.deps.DB.WithContext(ctx).Where("item_id = ?", itemID).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[string]models.StorageMigrationObject, len(rows))
	for _, row := range rows {
		result[row.ObjectKey] = row
	}
	return result, nil
}

func (w *WorkerGroup) persistStorageMigrationCopyProgress(ctx context.Context, item *models.StorageMigrationItem, progress *storageMigrationCopyProgress) error {
	pending := append([]models.StorageMigrationObject(nil), progress.pending...)
	for index := range pending {
		pending[index].ItemID = item.ID
	}
	err := w.deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(pending) > 0 {
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "item_id"}, {Name: "object_key"}},
				DoUpdates: clause.AssignmentColumns([]string{"source_revision", "source_size", "destination_e_tag", "checksum", "updated_at"}),
			}).Create(&pending).Error; err != nil {
				return err
			}
		}
		return updateActiveStorageMigrationItem(tx, item, map[string]any{
			"bytes_copied": progress.verifiedBytes, "objects_verified": progress.verifiedObjects,
			"progress_message": fmt.Sprintf("Verified %d of %d objects", progress.verifiedObjects, progress.totalObjects),
		})
	})
	if err != nil {
		return err
	}
	progress.pending = progress.pending[:0]
	progress.lastPersistedAt = time.Now()
	return w.refreshStorageMigrationProgress(ctx, item.MigrationID)
}

func (w *WorkerGroup) clearStorageMigrationObjectCheckpoints(ctx context.Context, itemID uint) error {
	return w.deps.DB.WithContext(ctx).Where("item_id = ?", itemID).Delete(&models.StorageMigrationObject{}).Error
}

func (w *WorkerGroup) refreshStorageMigrationProgress(ctx context.Context, migrationID uint) error {
	var aggregate struct {
		ActualBytes  int64
		CopiedBytes  int64
		CutoverCount int64
		CleanedCount int64
		DeletedCount int64
		FileCount    int64
	}
	err := w.deps.DB.WithContext(ctx).Model(&models.StorageMigrationItem{}).
		Select(`COALESCE(SUM(CASE WHEN status = ? THEN 0 WHEN bytes_total > 0 THEN bytes_total ELSE planned_bytes END), 0) AS actual_bytes,
			COALESCE(SUM(CASE WHEN status = ? THEN 0 ELSE bytes_copied END), 0) AS copied_bytes,
			SUM(CASE WHEN status IN ? THEN 1 ELSE 0 END) AS cutover_count,
			SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS cleaned_count,
			SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS deleted_count,
			COUNT(*) AS file_count`, models.StorageMigrationItemDeleted, models.StorageMigrationItemDeleted, []string{models.StorageMigrationItemCleanupPending, models.StorageMigrationItemCleaning, models.StorageMigrationItemCleaned, models.StorageMigrationItemOriginalKept, models.StorageMigrationItemOriginalPartial}, models.StorageMigrationItemCleaned, models.StorageMigrationItemDeleted).
		Where("migration_id = ?", migrationID).Scan(&aggregate).Error
	if err != nil {
		return err
	}
	if err := w.deps.DB.WithContext(ctx).Model(&models.StorageMigration{}).Where("id = ?", migrationID).Updates(map[string]any{
		"actual_bytes": aggregate.ActualBytes, "copied_bytes": aggregate.CopiedBytes,
		"cutover_count": aggregate.CutoverCount, "cleaned_count": aggregate.CleanedCount, "deleted_count": aggregate.DeletedCount,
	}).Error; err != nil {
		return err
	}
	progress := 0.0
	if aggregate.ActualBytes > 0 {
		progress = float64(aggregate.CopiedBytes) / float64(aggregate.ActualBytes)
	} else if aggregate.FileCount > 0 {
		progress = float64(aggregate.CutoverCount+aggregate.DeletedCount) / float64(aggregate.FileCount)
	}
	message := fmt.Sprintf("%d of %d videos active on destination", aggregate.CutoverCount, aggregate.FileCount)
	if aggregate.DeletedCount > 0 {
		message = fmt.Sprintf("%d moved, %d deleted during migration", aggregate.CutoverCount, aggregate.DeletedCount)
	}
	background.ReportProgress(ctx, progress, message)
	return nil
}

func (w *WorkerGroup) scheduleStorageMigrationCleanup(ctx context.Context, runtime *background.Runtime, migration *models.StorageMigration) error {
	if runtime == nil {
		return errors.New("background runtime is unavailable")
	}
	if err := w.deps.DB.WithContext(ctx).First(migration, migration.ID).Error; err != nil {
		return err
	}
	if migration.FileCount > 0 && migration.DeletedCount >= migration.FileCount {
		completedAt := time.Now().UTC()
		return w.deps.DB.WithContext(ctx).Model(migration).Updates(map[string]any{
			"status": models.StorageMigrationCompleted, "phase": "Migration complete; all videos were deleted",
			"copy_completed_at": &completedAt, "completed_at": &completedAt, "cleanup_after": nil,
		}).Error
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
	if err := w.deps.DB.WithContext(ctx).Where("migration_id = ? AND status NOT IN ?", migration.ID, []string{models.StorageMigrationItemCleaned, models.StorageMigrationItemDeleted}).Order("id ASC").Find(&items).Error; err != nil {
		return background.Result{}, background.Transient("cleanup_items_failed", "Cleanup videos could not be loaded", err)
	}
	for index := range items {
		if err := w.cleanupStorageMigrationItem(ctx, &items[index]); err != nil {
			if errors.Is(err, errStorageMigrationEncodingActive) {
				_ = w.deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&migration).Updates(map[string]any{
					"status": models.StorageMigrationCleaningOriginals,
					"phase":  "Waiting for video encoding to finish",
				}).Error
				background.ReportProgress(ctx, float64(index)/float64(len(items)), "Waiting for video encoding to finish")
				return background.Result{}, background.Deferred(
					"encoding_active",
					"Original cleanup is waiting for video encoding to finish",
					storageMigrationEncodingRetry,
				)
			}
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
	if err := w.refreshStorageMigrationProgress(context.WithoutCancel(ctx), migration.ID); err != nil {
		return background.Result{}, background.Transient("cleanup_progress_failed", "Original cleanup progress could not be finalized", err)
	}
	if err := w.deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&migration).Updates(map[string]any{
		"status": models.StorageMigrationCompleted, "phase": "Migration and cleanup complete",
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
	if item.Status == models.StorageMigrationItemCleaned || item.Status == models.StorageMigrationItemOriginalKept || item.Status == models.StorageMigrationItemOriginalPartial || item.Status == models.StorageMigrationItemDeleted {
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
	if item.Status == models.StorageMigrationItemCleaned || item.Status == models.StorageMigrationItemOriginalKept || item.Status == models.StorageMigrationItemOriginalPartial || item.Status == models.StorageMigrationItemDeleted {
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
	if err == nil {
		encoding, encodingErr := storageMigrationFileEncodingActive(w.deps.DB.WithContext(ctx), item.FileID)
		if encodingErr != nil {
			return encodingErr
		}
		if encoding {
			if updateErr := updateActiveStorageMigrationItem(w.deps.DB.WithContext(ctx), item, map[string]any{
				"status":           models.StorageMigrationItemCleanupPending,
				"progress_message": "Waiting for video encoding to finish",
			}); updateErr != nil && !errors.Is(updateErr, errStorageMigrationItemDeleted) {
				return updateErr
			}
			return errStorageMigrationEncodingActive
		}
	}
	store, err := w.deps.Storage.Store(item.SourceMountID)
	if err != nil {
		return err
	}
	prefix, err := w.deps.Storage.Layout().FilePrefix(item.FileUUID)
	if err != nil {
		return err
	}
	if err := updateActiveStorageMigrationItem(w.deps.DB.WithContext(ctx), item, map[string]any{
		"status": models.StorageMigrationItemCleaning, "progress_message": "Removing original copy",
	}); err != nil {
		if errors.Is(err, errStorageMigrationItemDeleted) {
			return nil
		}
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
	err = updateActiveStorageMigrationItem(w.deps.DB.WithContext(commitCtx), item, map[string]any{
		"status": models.StorageMigrationItemCleaned, "cleaned_at": &now, "reservation_key": "",
		"progress_message": "Original removed", "error_code": "", "error_message": "",
	})
	if errors.Is(err, errStorageMigrationItemDeleted) {
		return nil
	}
	return err
}

func storageMigrationFileEncodingActive(db *gorm.DB, fileID uint) (bool, error) {
	for _, model := range []any{&models.Quality{}, &models.Audio{}, &models.Subtitle{}} {
		var count int64
		if err := db.Model(model).Where("file_id = ? AND encoding = ?", fileID, true).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
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
	if item.Status == models.StorageMigrationItemDeleted {
		return w.clearStorageMigrationObjectCheckpoints(context.WithoutCancel(ctx), item.ID)
	}
	var file models.File
	err := w.deps.DB.WithContext(ctx).First(&file, item.FileID).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err == nil && file.StorageID == item.DestinationMountID && item.CutoverAt != nil {
		err := updateActiveStorageMigrationItem(w.deps.DB.WithContext(context.WithoutCancel(ctx)), item, map[string]any{
			"status": models.StorageMigrationItemOriginalKept, "reservation_key": "",
			"progress_message": "Destination remains active; original retained",
		})
		if errors.Is(err, errStorageMigrationItemDeleted) {
			return nil
		}
		return err
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
	err = w.deps.DB.WithContext(context.WithoutCancel(ctx)).Transaction(func(tx *gorm.DB) error {
		if err := updateActiveStorageMigrationItem(tx, item, map[string]any{
			"status": models.StorageMigrationItemCanceled, "reservation_key": "", "destination_owned": false,
			"bytes_copied": 0, "objects_verified": 0, "progress_message": "Canceled before cutover",
		}); err != nil {
			return err
		}
		return tx.Where("item_id = ?", item.ID).Delete(&models.StorageMigrationObject{}).Error
	})
	if errors.Is(err, errStorageMigrationItemDeleted) {
		return nil
	}
	return err
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
