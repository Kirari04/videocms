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
	var notReferencedFiles []uint
	if res := w.deps.DB.
		Raw(`
		SELECT files.id FROM files
		JOIN links ON links.file_id = files.id
		GROUP BY files.id
		HAVING COUNT(links.id) = SUM(CASE WHEN links.deleted_at IS NULL THEN 0 ELSE 1 END);
		`).Scan(&notReferencedFiles); res.Error != nil {
		log.Printf("Failed to query unreferenced files: %v", res.Error)
		return res.Error
	}

	if len(notReferencedFiles) > 0 {
		if res := w.deps.DB.Delete(&models.File{}, notReferencedFiles); res.Error != nil {
			log.Printf("Failed to delete unreferenced files: %v", res.Error)
			return res.Error
		}
	}

	var todos []models.File
	if res := w.deps.DB.
		Model(&models.File{}).
		Preload("Qualitys").
		Preload("Subtitles").
		Preload("Audios").
		Preload("Links").
		Unscoped().
		Where("deleted_at IS NOT NULL").
		Find(&todos, todos); res.Error != nil {
		log.Printf("Failed to query deleted files: %v", res.Error)
		return res.Error
	}

	if len(todos) > 0 {
		log.Printf("Queued %d file to delete", len(todos))
	}
	var skippingDeletion int
	var successDeletion int
	for _, todo := range todos {
		w.CancelDownloadPreparationsForFile(todo.ID)
		w.cancelActiveEncodingsForFile(todo.ID, "file queued for deletion")
		releaseFile := w.deps.StorageLifecycle.FileWriteLock(todo.ID)
		if err := w.deps.DB.Unscoped().Preload("Qualitys").Preload("Subtitles").Preload("Audios").Preload("Links").First(&todo, todo.ID).Error; err != nil {
			releaseFile()
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				deletionErrors = append(deletionErrors, err)
			}
			continue
		}
		if w.HasActiveDownloadPreparationForFile(todo.ID) {
			skippingDeletion++
			releaseFile()
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
			continue
		}

		if err := w.deleteStoredFile(todo); err != nil {
			log.Printf("Failed to delete stored data for file %d: %v", todo.ID, err)
			deletionErrors = append(deletionErrors, err)
			skippingDeletion++
			releaseFile()
			continue
		}

		if err := w.deps.DB.Transaction(func(tx *gorm.DB) error {
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
			continue
		}
		successDeletion++
		releaseFile()
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
	releaseMount := w.deps.StorageLifecycle.ReadLock(file.StorageID)
	defer releaseMount()
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
		store, err := w.deps.Storage.StoreOrDefault(file.StorageID)
		if err != nil {
			return err
		}
		prefix, err := w.deps.Storage.Layout().FilePrefix(file.UUID)
		if err != nil {
			return err
		}
		return storage.DeletePrefix(context.Background(), store, prefix)
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
