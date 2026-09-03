package logic

import (
	"context"
	"errors"
	"fmt"

	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"

	"gorm.io/gorm"
)

type storagePlacement struct {
	UUID      string
	UsedBytes int64
}

// UploadStoreCandidates returns mounted pool members from least to most bytes
// currently tracked by VideoCMS. The file keeps the actual selected mount ID,
// so later pool edits never change where existing media is read from.
func (s *Service) UploadStoreCandidates(userID uint) ([]string, error) {
	_, candidates, err := s.uploadStoreCandidates(userID)
	return candidates, err
}

func (s *Service) uploadStoreCandidates(userID uint) (uint, []string, error) {
	if s == nil || s.Deps == nil || s.Deps.DB == nil || s.Deps.Storage == nil {
		return 0, nil, storage.ErrStoreNotConfigured
	}
	var user struct {
		StoragePoolID *uint
	}
	if err := s.Deps.DB.Model(&models.User{}).Select("storage_pool_id").First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil, errors.New("user not found")
		}
		return 0, nil, fmt.Errorf("load user storage pool: %w", err)
	}
	poolID := uint(0)
	if user.StoragePoolID != nil {
		poolID = *user.StoragePoolID
	} else {
		var defaultPool models.StoragePool
		if err := s.Deps.DB.Where("is_default = ?", true).Order("id").First(&defaultPool).Error; err != nil {
			return 0, nil, fmt.Errorf("load default storage pool: %w", err)
		}
		poolID = defaultPool.ID
	}

	var placements []storagePlacement
	err := s.Deps.DB.Table("storage_pool_mounts AS pool_mounts").
		Select(`storage_mounts.uuid,
			COALESCE(SUM(CASE
				WHEN files.deleted_at IS NULL AND files.storage_state = ? THEN files.size
				ELSE 0
			END), 0) AS used_bytes`, models.FileStorageAvailable).
		Joins("JOIN storage_mounts ON storage_mounts.id = pool_mounts.storage_mount_id").
		Joins("LEFT JOIN files ON files.storage_id = storage_mounts.uuid").
		Where("pool_mounts.storage_pool_id = ? AND pool_mounts.role = ? AND storage_mounts.mounted = ? AND (storage_mounts.last_error IS NULL OR storage_mounts.last_error = '')", poolID, models.StoragePoolMountPrimary, true).
		Group("storage_mounts.id, storage_mounts.uuid").
		Order("used_bytes ASC, storage_mounts.uuid ASC").
		Scan(&placements).Error
	if err != nil {
		return 0, nil, fmt.Errorf("select storage pool member: %w", err)
	}
	candidates := make([]string, 0, len(placements))
	for _, placement := range placements {
		if _, err := s.Deps.Storage.Store(placement.UUID); err == nil {
			candidates = append(candidates, placement.UUID)
		}
	}
	if len(candidates) == 0 {
		return 0, nil, errors.New("selected storage pool has no available primary mounts")
	}
	return poolID, candidates, nil
}

// publishUploadSource keeps the selected mount stable until its caller has
// committed the owning file record. Detach waits for the returned release
// function, then marks that newly committed record unavailable with the rest
// of the mount. Failed candidates are cleaned up before placement falls back.
func (s *Service) publishUploadSource(ctx context.Context, userID uint, key storage.Key, localPath string) (string, func(), error) {
	storeID, _, release, err := s.publishUploadSourceWithPool(ctx, userID, key, localPath)
	return storeID, release, err
}

func (s *Service) publishUploadSourceWithPool(ctx context.Context, userID uint, key storage.Key, localPath string) (string, uint, func(), error) {
	poolID, candidates, err := s.uploadStoreCandidates(userID)
	if err != nil {
		return "", 0, nil, err
	}
	var publishErr error
	for _, candidate := range candidates {
		release := s.Deps.StorageLifecycle.ReadLock(candidate)
		_, err := s.Deps.Storage.PublishFile(ctx, candidate, key, localPath, storage.PutOptions{})
		if err == nil {
			return candidate, poolID, release, nil
		}
		if store, storeErr := s.Deps.Storage.Store(candidate); storeErr == nil {
			if cleanupErr := store.Delete(context.WithoutCancel(ctx), key); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("clean failed upload from %s: %w", candidate, cleanupErr))
			}
		}
		release()
		publishErr = errors.Join(publishErr, fmt.Errorf("%s: %w", candidate, err))
	}
	return "", 0, nil, publishErr
}
