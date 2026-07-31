package cmd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"

	"gorm.io/gorm"
)

func newStorageService(ctx context.Context, cfg config.Config, db *gorm.DB) (*storage.Service, *storage.CredentialCipher, error) {
	if db == nil {
		return nil, nil, errors.New("storage database is not configured")
	}
	localMediaStore, err := storage.NewLocalStore(cfg.FolderVideoQualitysPriv)
	if err != nil {
		return nil, nil, err
	}
	closeLocalOnError := true
	defer func() {
		if closeLocalOnError {
			_ = localMediaStore.Close()
		}
	}()
	workspace, err := storage.NewLocalWorkspace(cfg.StorageScratchDir)
	if err != nil {
		return nil, nil, err
	}
	storageService, err := storage.NewServiceWithWorkspace(
		models.StorageMountLocalUUID,
		storage.LegacyMediaLayout{},
		workspace,
		map[string]storage.Store{models.StorageMountLocalUUID: localMediaStore},
	)
	if err != nil {
		return nil, nil, err
	}

	var credentialCipher *storage.CredentialCipher
	if cfg.StorageEncryptionKey != "" {
		credentialCipher, err = storage.NewCredentialCipher(cfg.StorageEncryptionKey)
		if err != nil {
			return nil, nil, err
		}
	}
	var mounts []models.StorageMount
	if err := db.Where("mounted = ? AND uuid <> ?", true, models.StorageMountLocalUUID).Find(&mounts).Error; err != nil {
		return nil, nil, fmt.Errorf("load configured storage mounts: %w", err)
	}
	for i := range mounts {
		mount := &mounts[i]
		if credentialCipher == nil {
			markStorageMountLoadFailure(db, mount, storage.ErrEncryptionKeyNotConfigured)
			continue
		}
		var store storage.Store
		switch mount.Provider {
		case models.StorageProviderS3:
			store, err = storage.NewS3StoreFromMount(ctx, mount.UUID, mount.Configuration, mount.EncryptedCredentials, credentialCipher)
		default:
			err = fmt.Errorf("unsupported storage provider %q", mount.Provider)
		}
		if err != nil {
			markStorageMountLoadFailure(db, mount, err)
			continue
		}
		if _, err := storageService.RegisterStore(mount.UUID, store); err != nil {
			_ = store.Close()
			markStorageMountLoadFailure(db, mount, err)
			continue
		}
		if mount.LastError != "" {
			_ = db.Model(mount).Update("last_error", "").Error
		}
		reconnectCtx, cancelReconnect := context.WithTimeout(ctx, 30*time.Second)
		reconnectErr := reconnectMountedStorageFiles(reconnectCtx, db, storageService, *mount)
		cancelReconnect()
		if reconnectErr != nil {
			log.Printf("failed to reconnect files for storage mount %s: %v", mount.UUID, reconnectErr)
			_ = db.Model(mount).Update("last_error", "Reconnect scan failed: "+reconnectErr.Error()).Error
		}
	}
	closeLocalOnError = false
	return storageService, credentialCipher, nil
}

func reconnectMountedStorageFiles(ctx context.Context, db *gorm.DB, storageService *storage.Service, mount models.StorageMount) error {
	service := logic.NewService(&app.Deps{DB: db, Storage: storageService})
	result, err := service.ReconnectStorageMountFiles(ctx, mount.ID, true, mount.UUID)
	if result.Relinked > 0 {
		log.Printf("reconnected %d files to storage mount %s during startup", result.Relinked, mount.UUID)
	}
	return err
}

func markStorageMountLoadFailure(db *gorm.DB, mount *models.StorageMount, loadErr error) {
	message := loadErr.Error()
	log.Printf("storage mount %s is unavailable: %v", mount.UUID, loadErr)
	_ = db.Model(mount).Update("last_error", message).Error
	_ = db.Model(&models.File{}).
		Where("storage_id = ?", mount.UUID).
		Updates(map[string]any{
			"storage_state": models.FileStorageUnavailable,
		}).Error
}
