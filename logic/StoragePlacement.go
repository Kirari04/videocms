package logic

import (
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
	if s == nil || s.Deps == nil || s.Deps.DB == nil || s.Deps.Storage == nil {
		return nil, storage.ErrStoreNotConfigured
	}
	var user struct {
		StoragePoolID *uint
	}
	if err := s.Deps.DB.Model(&models.User{}).Select("storage_pool_id").First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("user not found")
		}
		return nil, fmt.Errorf("load user storage pool: %w", err)
	}
	poolID := uint(0)
	if user.StoragePoolID != nil {
		poolID = *user.StoragePoolID
	} else {
		var defaultPool models.StoragePool
		if err := s.Deps.DB.Where("is_default = ?", true).Order("id").First(&defaultPool).Error; err != nil {
			return nil, fmt.Errorf("load default storage pool: %w", err)
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
		Where("pool_mounts.storage_pool_id = ? AND storage_mounts.mounted = ?", poolID, true).
		Group("storage_mounts.id, storage_mounts.uuid").
		Order("used_bytes ASC, storage_mounts.uuid ASC").
		Scan(&placements).Error
	if err != nil {
		return nil, fmt.Errorf("select storage pool member: %w", err)
	}
	candidates := make([]string, 0, len(placements))
	for _, placement := range placements {
		if _, err := s.Deps.Storage.Store(placement.UUID); err == nil {
			candidates = append(candidates, placement.UUID)
		}
	}
	if len(candidates) == 0 {
		return nil, errors.New("selected storage pool has no available mounts")
	}
	return candidates, nil
}
