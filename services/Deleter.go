package services

import (
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"
	"context"
	"errors"
	"log"
	"os"
	"time"

	"gorm.io/gorm"
)

func (w *WorkerGroup) Deleter(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		_ = w.runDeleter()
		if !sleepContext(ctx, time.Second*20) {
			return
		}
	}
}

func (w *WorkerGroup) runDeleter() error {
	var deletionErrors []error
	var candidateFileIDs []uint
	if res := w.deps.DB.
		Raw(`
		SELECT files.id
		FROM files
		WHERE files.deleted_at IS NOT NULL
			OR NOT EXISTS (
				SELECT 1 FROM links
				WHERE links.file_id = files.id AND links.deleted_at IS NULL
			)
		ORDER BY files.id ASC;
		`).Scan(&candidateFileIDs); res.Error != nil {
		log.Printf("Failed to query unreferenced files: %v", res.Error)
		return res.Error
	}

	if len(candidateFileIDs) > 0 {
		log.Printf("Queued %d file to delete", len(candidateFileIDs))
	}
	var skippingDeletion int
	var successDeletion int
	for _, fileID := range candidateFileIDs {
		w.CancelDownloadPreparationsForFile(fileID)
		w.cancelActiveEncodingsForFile(fileID, "file queued for deletion")
		releaseFile := w.deps.StorageLifecycle.FileWriteLock(fileID)
		var todo models.File
		if err := w.deps.DB.Unscoped().Preload("Qualitys").Preload("Subtitles").Preload("Audios").Preload("Links").First(&todo, fileID).Error; err != nil {
			releaseFile()
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				deletionErrors = append(deletionErrors, err)
			}
			continue
		}
		if todo.DeletedAt == nil || !todo.DeletedAt.Valid {
			var references int64
			if err := w.deps.DB.Model(&models.Link{}).Where("file_id = ?", todo.ID).Count(&references).Error; err != nil {
				deletionErrors = append(deletionErrors, err)
				releaseFile()
				continue
			}
			if references > 0 {
				releaseFile()
				continue
			}
			if err := w.deps.DB.Delete(&todo).Error; err != nil {
				deletionErrors = append(deletionErrors, err)
				releaseFile()
				continue
			}
		}

		var migrationItems []models.StorageMigrationItem
		if err := w.deps.DB.Where("file_id = ?", todo.ID).Order("id ASC").Find(&migrationItems).Error; err != nil {
			deletionErrors = append(deletionErrors, err)
			releaseFile()
			continue
		}
		migrationIDs := make(map[uint]struct{}, len(migrationItems))
		if len(migrationItems) > 0 {
			if err := w.deps.DB.Model(&models.StorageMigrationItem{}).Where("file_id = ?", todo.ID).Updates(map[string]any{
				"status": models.StorageMigrationItemDeleted, "progress_message": "Deleted during migration",
				"error_code": "", "error_message": "", "bytes_total": 0, "bytes_copied": 0,
				"objects_verified": 0,
			}).Error; err != nil {
				deletionErrors = append(deletionErrors, err)
				releaseFile()
				continue
			}
			for index := range migrationItems {
				migrationIDs[migrationItems[index].MigrationID] = struct{}{}
			}
		}
		if w.HasActiveDownloadPreparationForFile(todo.ID) {
			skippingDeletion++
			releaseFile()
			w.refreshDeletedFileMigrationProgress(migrationIDs, &deletionErrors)
			continue
		}

		/**
		 * check if all files qualities, subs & audios are not currently encoding because else there might be
		* parallel to the delete command an active ffmpeg conversion running
		*/
		encoding := false
		for _, quality := range todo.Qualitys {
			if quality.Encoding {
				encoding = true
			}
		}
		for _, audio := range todo.Audios {
			if audio.Encoding {
				encoding = true
			}
		}
		for _, sub := range todo.Subtitles {
			if sub.Encoding {
				encoding = true
			}
		}

		if encoding {
			// we will try again in the next loop (the encoding process may be finished until then)
			skippingDeletion++
			releaseFile()
			w.refreshDeletedFileMigrationProgress(migrationIDs, &deletionErrors)
			continue
		}

		if err := w.deleteStoredFileCopies(todo, migrationItems); err != nil {
			log.Printf("Failed to delete stored data for file %d: %v", todo.ID, err)
			deletionErrors = append(deletionErrors, err)
			skippingDeletion++
			releaseFile()
			w.refreshDeletedFileMigrationProgress(migrationIDs, &deletionErrors)
			continue
		}

		if err := w.deps.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&models.StorageMigrationItem{}).Where("file_id = ?", todo.ID).Updates(map[string]any{
				"status": models.StorageMigrationItemDeleted, "reservation_key": "", "destination_owned": false,
				"progress_message": "Deleted during migration", "error_code": "", "error_message": "",
				"bytes_total": 0, "bytes_copied": 0, "objects_verified": 0,
			}).Error; err != nil {
				return err
			}
			if err := tx.Unscoped().Where("file_id = ?", todo.ID).Delete(&models.Subtitle{}).Error; err != nil {
				return err
			}
			if err := tx.Unscoped().Where("file_id = ?", todo.ID).Delete(&models.Audio{}).Error; err != nil {
				return err
			}
			if err := tx.Unscoped().Where("file_id = ?", todo.ID).Delete(&models.Quality{}).Error; err != nil {
				return err
			}
			return tx.Unscoped().Delete(&todo).Error
		}); err != nil {
			log.Printf("Failed to delete file %d from database: %v", todo.ID, err)
			deletionErrors = append(deletionErrors, err)
			releaseFile()
			w.refreshDeletedFileMigrationProgress(migrationIDs, &deletionErrors)
			continue
		}
		successDeletion++
		releaseFile()
		w.refreshDeletedFileMigrationProgress(migrationIDs, &deletionErrors)
	}
	if skippingDeletion > 0 {
		log.Printf("Skipped %d files from deletion", skippingDeletion)
	}
	if successDeletion > 0 {
		log.Printf("Successfully deleted %d files", successDeletion)
	}
	return errors.Join(deletionErrors...)
}

func (w *WorkerGroup) deleteStoredFile(file models.File) error {
	return w.deleteStoredFileCopies(file, nil)
}

func (w *WorkerGroup) deleteStoredFileCopies(file models.File, migrationItems []models.StorageMigrationItem) error {
	mountIDs := []string{file.StorageID}
	if w.deps.Storage != nil && w.deps.Storage.Layout() != nil {
		for _, item := range migrationItems {
			if item.CleanedAt == nil {
				mountIDs = append(mountIDs, item.SourceMountID)
			}
			if item.DestinationOwned {
				mountIDs = append(mountIDs, item.DestinationMountID)
			}
		}
	}
	releaseMounts := w.deps.StorageLifecycle.ReadLocks(mountIDs...)
	defer releaseMounts()
	if file.Path != "" {
		info, err := os.Stat(file.Path)
		switch {
		case err == nil && !info.IsDir():
			if err := os.Remove(file.Path); err != nil {
				return err
			}
		case err != nil && !os.IsNotExist(err):
			return err
		}
	}

	if w.deps.Storage != nil && w.deps.Storage.Layout() != nil {
		prefix, err := w.deps.Storage.Layout().FilePrefix(file.UUID)
		if err != nil {
			return err
		}
		seen := make(map[string]bool, len(mountIDs))
		for _, mountID := range mountIDs {
			if seen[mountID] {
				continue
			}
			seen[mountID] = true
			store, err := w.deps.Storage.StoreOrDefault(mountID)
			if err != nil {
				return err
			}
			if err := storage.DeletePrefix(context.Background(), store, prefix); err != nil {
				return err
			}
		}
		return nil
	}

	if file.Folder == "" {
		return nil
	}
	info, err := os.Stat(file.Folder)
	switch {
	case os.IsNotExist(err):
		return nil
	case err != nil:
		return err
	case !info.IsDir():
		return nil
	default:
		return os.RemoveAll(file.Folder)
	}
}

func (w *WorkerGroup) refreshDeletedFileMigrationProgress(migrationIDs map[uint]struct{}, deletionErrors *[]error) {
	for migrationID := range migrationIDs {
		if err := w.refreshStorageMigrationProgress(context.Background(), migrationID); err != nil {
			*deletionErrors = append(*deletionErrors, err)
		}
	}
}
