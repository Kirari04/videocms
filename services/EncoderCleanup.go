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

func (w *WorkerGroup) EncoderCleanup(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		_ = w.runEncoderCleanup()
		if !sleepContext(ctx, time.Minute) {
			return
		}
	}
}

/*
This function deletes the originally uploaded file after all qualitys and subtitles were encoded
*/
func (w *WorkerGroup) runEncoderCleanup() error {
	var cleanupErrors []error
	var dbReadyFiles []models.File
	if res := w.deps.DB.
		Preload("Qualitys").
		Preload("Subtitles").
		Preload("Audios").
		Where("storage_state = ?", models.FileStorageAvailable).
		Where("path <> '' OR source_key <> ''").
		Find(&dbReadyFiles); res.Error != nil {
		log.Printf("Failed to get PossibleDeleteTargets: %v", res.Error)
		return res.Error
	}

	for _, dbReadyFile := range dbReadyFiles {
		releaseFile := w.deps.StorageLifecycle.FileWriteLock(dbReadyFile.ID)
		if res := w.deps.DB.
			Preload("Qualitys").Preload("Subtitles").Preload("Audios").
			Where("storage_state = ?", models.FileStorageAvailable).First(&dbReadyFile, dbReadyFile.ID); res.Error != nil {
			releaseFile()
			if !errors.Is(res.Error, gorm.ErrRecordNotFound) {
				cleanupErrors = append(cleanupErrors, res.Error)
			}
			continue
		}
		releaseMount := w.deps.StorageLifecycle.ReadLock(dbReadyFile.StorageID)
		var qualityAmount int64
		if res := w.deps.DB.
			Model(&models.Quality{}).
			Where(&models.Quality{
				FileID: dbReadyFile.ID,
				Ready:  true,
			}).
			Count(&qualityAmount); res.Error != nil {
			log.Printf("Failed to count quality by (delete candidate): Searcher ID %d inside database. Error: %v", dbReadyFile.ID, res.Error)
			cleanupErrors = append(cleanupErrors, res.Error)
			releaseMount()
			releaseFile()
			continue
		}

		var subtitleAmount int64
		if res := w.deps.DB.
			Model(&models.Subtitle{}).
			Where(&models.Subtitle{
				FileID: dbReadyFile.ID,
				Ready:  true,
			}).
			Count(&subtitleAmount); res.Error != nil {
			log.Printf("Failed to count subtitle by (delete candidate): Searcher ID %d inside database. Error: %v", dbReadyFile.ID, res.Error)
			cleanupErrors = append(cleanupErrors, res.Error)
			releaseMount()
			releaseFile()
			continue
		}

		var audioAmount int64
		if res := w.deps.DB.
			Model(&models.Audio{}).
			Where(&models.Audio{
				FileID: dbReadyFile.ID,
				Ready:  true,
			}).
			Count(&audioAmount); res.Error != nil {
			log.Printf("Failed to count audio by (delete candidate): Searcher ID %d inside database. Error: %v", dbReadyFile.ID, res.Error)
			cleanupErrors = append(cleanupErrors, res.Error)
			releaseMount()
			releaseFile()
			continue
		}

		// in case all qualitys are encoded or failed the original file can be deleted
		if qualityAmount == int64(len(dbReadyFile.Qualitys)) &&
			subtitleAmount == int64(len(dbReadyFile.Subtitles)) &&
			audioAmount == int64(len(dbReadyFile.Audios)) {
			if dbReadyFile.SourceKey != "" {
				if w.deps.Storage == nil {
					log.Printf("Failed to delete source object for file %d: storage is not configured", dbReadyFile.ID)
					cleanupErrors = append(cleanupErrors, storage.ErrStoreNotConfigured)
					releaseMount()
					releaseFile()
					continue
				}
				store, err := w.deps.Storage.StoreOrDefault(dbReadyFile.StorageID)
				if err != nil {
					log.Printf("Failed to resolve source store for file %d: %v", dbReadyFile.ID, err)
					cleanupErrors = append(cleanupErrors, err)
					releaseMount()
					releaseFile()
					continue
				}
				key, err := storage.ParseKey(dbReadyFile.SourceKey)
				if err != nil {
					log.Printf("Failed to parse source key for file %d: %v", dbReadyFile.ID, err)
					cleanupErrors = append(cleanupErrors, err)
					releaseMount()
					releaseFile()
					continue
				}
				if err := store.Delete(context.Background(), key); err != nil {
					log.Printf("Failed to delete source object %s: %v", dbReadyFile.SourceKey, err)
					cleanupErrors = append(cleanupErrors, err)
					releaseMount()
					releaseFile()
					continue
				}
			}
			if dbReadyFile.Path != "" {
				if err := os.Remove(dbReadyFile.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
					log.Printf("Failed to delete file from path (%v): %v", dbReadyFile.Path, err)
					cleanupErrors = append(cleanupErrors, err)
					releaseMount()
					releaseFile()
					continue
				}
			}

			// overwrite total filesize in file
			newSize, err := w.storedFileSize(context.Background(), dbReadyFile)
			if err != nil {
				log.Printf("Failed to calculate stored size after cleanup: %v", err)
				cleanupErrors = append(cleanupErrors, err)
			} else {
				dbReadyFile.Size = newSize
			}
			dbReadyFile.Path = ""
			dbReadyFile.SourceKey = ""
			if err := w.deps.DB.Save(&dbReadyFile).Error; err != nil {
				cleanupErrors = append(cleanupErrors, err)
			}
		}
		releaseMount()
		releaseFile()
	}
	return errors.Join(cleanupErrors...)
}

func (w *WorkerGroup) storedFileSize(ctx context.Context, file models.File) (int64, error) {
	if w.deps.Storage == nil || w.deps.Storage.Layout() == nil {
		return dirSize(file.Folder)
	}
	store, err := w.deps.Storage.StoreOrDefault(file.StorageID)
	if err != nil {
		return 0, err
	}
	prefix, err := w.deps.Storage.Layout().FilePrefix(file.UUID)
	if err != nil {
		return 0, err
	}
	var size int64
	err = store.Walk(ctx, prefix, func(info storage.ObjectInfo) error {
		size += info.Size
		return nil
	})
	return size, err
}
