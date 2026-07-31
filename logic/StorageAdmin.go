package logic

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type S3StorageMountInput struct {
	Name          string
	Configuration storage.S3MountConfiguration
	Credentials   *storage.S3MountCredentials
}

type StorageMountResponse struct {
	ID                    uint
	UUID                  string
	Name                  string
	Provider              string
	Mounted               bool
	System                bool
	Configuration         *storage.S3MountConfiguration `json:",omitempty"`
	CredentialsConfigured bool
	UsedBytes             int64
	FileCount             int64
	UnavailableFileCount  int64
	LastError             string
	LastCheckedAt         *time.Time
	UnmountedAt           *time.Time
}

type StoragePoolResponse struct {
	ID                uint
	UUID              string
	Name              string
	IsDefault         bool
	System            bool
	MountIDs          []uint
	UserOverrideCount int64
}

type StorageAdminOverview struct {
	EncryptionConfigured bool
	Mounts               []StorageMountResponse
	Pools                []StoragePoolResponse
}

type StoragePoolInput struct {
	Name      string
	MountIDs  []uint
	IsDefault bool
}

type StorageReconnectResult struct {
	Scanned  int
	Matched  int
	Relinked int
	Warning  string `json:",omitempty"`
}

type storageMountUsage struct {
	StorageID            string
	UsedBytes            int64
	FileCount            int64
	UnavailableFileCount int64
}

func (s *Service) StorageAdminOverview() (StorageAdminOverview, error) {
	if s == nil || s.Deps == nil || s.Deps.DB == nil {
		return StorageAdminOverview{}, errors.New("storage administration is not configured")
	}
	var mounts []models.StorageMount
	if err := s.Deps.DB.Order("system DESC, name ASC, id ASC").Find(&mounts).Error; err != nil {
		return StorageAdminOverview{}, err
	}
	var usages []storageMountUsage
	if err := s.Deps.DB.Model(&models.File{}).
		Select(`storage_id,
			COALESCE(SUM(CASE WHEN deleted_at IS NULL THEN size ELSE 0 END), 0) AS used_bytes,
			SUM(CASE WHEN deleted_at IS NULL THEN 1 ELSE 0 END) AS file_count,
			SUM(CASE WHEN deleted_at IS NULL AND storage_state = ? THEN 1 ELSE 0 END) AS unavailable_file_count`, models.FileStorageUnavailable).
		Unscoped().
		Group("storage_id").
		Scan(&usages).Error; err != nil {
		return StorageAdminOverview{}, err
	}
	usageByMount := make(map[string]storageMountUsage, len(usages))
	for _, usage := range usages {
		usageByMount[usage.StorageID] = usage
	}
	mountResponses := make([]StorageMountResponse, 0, len(mounts))
	for _, mount := range mounts {
		usage := usageByMount[mount.UUID]
		response := StorageMountResponse{
			ID:                    mount.ID,
			UUID:                  mount.UUID,
			Name:                  mount.Name,
			Provider:              mount.Provider,
			Mounted:               mount.Mounted,
			System:                mount.System,
			CredentialsConfigured: mount.EncryptedCredentials != "",
			UsedBytes:             usage.UsedBytes,
			FileCount:             usage.FileCount,
			UnavailableFileCount:  usage.UnavailableFileCount,
			LastError:             mount.LastError,
			LastCheckedAt:         mount.LastCheckedAt,
			UnmountedAt:           mount.UnmountedAt,
		}
		if mount.Provider == models.StorageProviderS3 && mount.Configuration != "" {
			configuration, err := storage.DecodeS3MountConfiguration(mount.Configuration)
			if err != nil {
				return StorageAdminOverview{}, fmt.Errorf("decode storage mount %s: %w", mount.UUID, err)
			}
			response.Configuration = &configuration
		}
		mountResponses = append(mountResponses, response)
	}

	var pools []models.StoragePool
	if err := s.Deps.DB.Preload("Members").Order("is_default DESC, system DESC, name ASC").Find(&pools).Error; err != nil {
		return StorageAdminOverview{}, err
	}
	poolResponses := make([]StoragePoolResponse, 0, len(pools))
	for _, pool := range pools {
		mountIDs := make([]uint, 0, len(pool.Members))
		for _, member := range pool.Members {
			mountIDs = append(mountIDs, member.StorageMountID)
		}
		sort.Slice(mountIDs, func(i, j int) bool { return mountIDs[i] < mountIDs[j] })
		var userCount int64
		if err := s.Deps.DB.Model(&models.User{}).Where("storage_pool_id = ?", pool.ID).Count(&userCount).Error; err != nil {
			return StorageAdminOverview{}, err
		}
		poolResponses = append(poolResponses, StoragePoolResponse{
			ID:                pool.ID,
			UUID:              pool.UUID,
			Name:              pool.Name,
			IsDefault:         pool.IsDefault,
			System:            pool.System,
			MountIDs:          mountIDs,
			UserOverrideCount: userCount,
		})
	}
	return StorageAdminOverview{
		EncryptionConfigured: s.Deps.StorageCipher != nil,
		Mounts:               mountResponses,
		Pools:                poolResponses,
	}, nil
}

func (s *Service) CreateS3StorageMount(ctx context.Context, input S3StorageMountInput) (models.StorageMount, StorageReconnectResult, error) {
	if s.Deps.StorageCipher == nil {
		return models.StorageMount{}, StorageReconnectResult{}, storage.ErrEncryptionKeyNotConfigured
	}
	if input.Credentials == nil {
		input.Credentials = &storage.S3MountCredentials{}
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return models.StorageMount{}, StorageReconnectResult{}, errors.New("storage mount name is required")
	}
	mount := models.StorageMount{
		UUID:     uuid.NewString(),
		Name:     input.Name,
		Provider: models.StorageProviderS3,
		Mounted:  true,
	}
	configuration, credentials, err := storage.EncodeS3Mount(input.Configuration, *input.Credentials, mount.UUID, s.Deps.StorageCipher)
	if err != nil {
		return models.StorageMount{}, StorageReconnectResult{}, err
	}
	mount.Configuration = configuration
	mount.EncryptedCredentials = credentials
	store, err := storage.NewS3StoreFromMount(ctx, mount.UUID, configuration, credentials, s.Deps.StorageCipher)
	if err != nil {
		return models.StorageMount{}, StorageReconnectResult{}, err
	}
	if err := checkStorageConnection(ctx, store); err != nil {
		_ = store.Close()
		return models.StorageMount{}, StorageReconnectResult{}, err
	}
	now := time.Now().UTC()
	mount.LastCheckedAt = &now
	if err := s.Deps.DB.Create(&mount).Error; err != nil {
		_ = store.Close()
		return models.StorageMount{}, StorageReconnectResult{}, err
	}
	if _, err := s.Deps.Storage.RegisterStore(mount.UUID, store); err != nil {
		_ = store.Close()
		_ = s.Deps.DB.Unscoped().Delete(&mount).Error
		return models.StorageMount{}, StorageReconnectResult{}, err
	}
	reconnect, reconnectErr := s.ReconnectStorageMount(ctx, mount.ID, true)
	if reconnectErr != nil {
		reconnect.Warning = reconnectErr.Error()
	}
	return mount, reconnect, nil
}

func (s *Service) UpdateS3StorageMount(ctx context.Context, mountID uint, input S3StorageMountInput) (models.StorageMount, error) {
	if s.Deps.StorageCipher == nil {
		return models.StorageMount{}, storage.ErrEncryptionKeyNotConfigured
	}
	var mount models.StorageMount
	if err := s.Deps.DB.First(&mount, mountID).Error; err != nil {
		return models.StorageMount{}, err
	}
	if mount.System || mount.Provider != models.StorageProviderS3 {
		return models.StorageMount{}, errors.New("storage mount cannot be edited")
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return models.StorageMount{}, errors.New("storage mount name is required")
	}
	credentials := storage.S3MountCredentials{}
	var err error
	if input.Credentials != nil {
		credentials = *input.Credentials
	} else {
		credentials, err = storage.DecodeS3MountCredentials(mount.EncryptedCredentials, mount.UUID, s.Deps.StorageCipher)
		if err != nil {
			return models.StorageMount{}, err
		}
	}
	configuration, encryptedCredentials, err := storage.EncodeS3Mount(input.Configuration, credentials, mount.UUID, s.Deps.StorageCipher)
	if err != nil {
		return models.StorageMount{}, err
	}
	store, err := storage.NewS3StoreFromMount(ctx, mount.UUID, configuration, encryptedCredentials, s.Deps.StorageCipher)
	if err != nil {
		return models.StorageMount{}, err
	}
	if err := checkStorageConnection(ctx, store); err != nil {
		_ = store.Close()
		return models.StorageMount{}, err
	}
	now := time.Now().UTC()
	updates := map[string]any{
		"name":                  input.Name,
		"configuration":         configuration,
		"encrypted_credentials": encryptedCredentials,
		"last_checked_at":       &now,
		"last_error":            "",
	}
	if err := s.Deps.DB.Model(&mount).Updates(updates).Error; err != nil {
		_ = store.Close()
		return models.StorageMount{}, err
	}
	mount.Name = input.Name
	mount.Configuration = configuration
	mount.EncryptedCredentials = encryptedCredentials
	mount.LastCheckedAt = &now
	mount.LastError = ""
	if mount.Mounted {
		if _, err := s.Deps.Storage.RegisterStore(mount.UUID, store); err != nil {
			_ = store.Close()
			return models.StorageMount{}, err
		}
	} else {
		_ = store.Close()
	}
	return mount, nil
}

func (s *Service) UnmountStorageMount(mountID uint) (int64, error) {
	var mount models.StorageMount
	if err := s.Deps.DB.First(&mount, mountID).Error; err != nil {
		return 0, err
	}
	if mount.System {
		return 0, errors.New("built-in local storage cannot be unmounted")
	}
	if !mount.Mounted {
		var count int64
		err := s.Deps.DB.Model(&models.File{}).Where("storage_id = ? AND storage_state = ?", mount.UUID, models.FileStorageUnavailable).Count(&count).Error
		return count, err
	}
	now := time.Now().UTC()
	var unavailable int64
	err := s.Deps.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&mount).Updates(map[string]any{
			"mounted":         false,
			"unmounted_at":    &now,
			"last_error":      "Unmounted by administrator",
			"last_checked_at": &now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.File{}).
			Where("storage_id = ? AND storage_state <> ?", mount.UUID, models.FileStorageUnavailable).
			Update("storage_state", models.FileStorageUnavailable).Error; err != nil {
			return err
		}
		return tx.Model(&models.File{}).
			Where("storage_id = ? AND storage_state = ?", mount.UUID, models.FileStorageUnavailable).
			Count(&unavailable).Error
	})
	if err != nil {
		return 0, err
	}
	if _, err := s.Deps.Storage.UnregisterStore(mount.UUID); err != nil && !errors.Is(err, storage.ErrStoreNotConfigured) {
		return unavailable, err
	}
	return unavailable, nil
}

func (s *Service) RemountStorageMount(ctx context.Context, mountID uint) (StorageReconnectResult, error) {
	if s.Deps.StorageCipher == nil {
		return StorageReconnectResult{}, storage.ErrEncryptionKeyNotConfigured
	}
	var mount models.StorageMount
	if err := s.Deps.DB.First(&mount, mountID).Error; err != nil {
		return StorageReconnectResult{}, err
	}
	if mount.System || mount.Provider != models.StorageProviderS3 {
		return StorageReconnectResult{}, errors.New("storage mount cannot be remounted")
	}
	store, err := storage.NewS3StoreFromMount(ctx, mount.UUID, mount.Configuration, mount.EncryptedCredentials, s.Deps.StorageCipher)
	if err != nil {
		return StorageReconnectResult{}, err
	}
	if err := checkStorageConnection(ctx, store); err != nil {
		_ = store.Close()
		return StorageReconnectResult{}, err
	}
	if _, err := s.Deps.Storage.RegisterStore(mount.UUID, store); err != nil {
		_ = store.Close()
		return StorageReconnectResult{}, err
	}
	now := time.Now().UTC()
	if err := s.Deps.DB.Model(&mount).Updates(map[string]any{
		"mounted":         true,
		"unmounted_at":    nil,
		"last_error":      "",
		"last_checked_at": &now,
	}).Error; err != nil {
		_, _ = s.Deps.Storage.UnregisterStore(mount.UUID)
		return StorageReconnectResult{}, err
	}
	reconnect, reconnectErr := s.reconnectStorageMount(ctx, mount.ID, true, mount.UUID)
	if reconnectErr != nil {
		reconnect.Warning = reconnectErr.Error()
	}
	return reconnect, nil
}

func (s *Service) CheckStorageMount(ctx context.Context, mountID uint) error {
	var mount models.StorageMount
	if err := s.Deps.DB.First(&mount, mountID).Error; err != nil {
		return err
	}
	store, err := s.Deps.Storage.Store(mount.UUID)
	if err != nil {
		return err
	}
	checkErr := checkStorageConnection(ctx, store)
	now := time.Now().UTC()
	message := ""
	if checkErr != nil {
		message = checkErr.Error()
	}
	_ = s.Deps.DB.Model(&mount).Updates(map[string]any{"last_checked_at": &now, "last_error": message}).Error
	return checkErr
}

var errStoragePrefixFound = errors.New("storage prefix found")

func (s *Service) ReconnectStorageMount(ctx context.Context, mountID uint, apply bool) (StorageReconnectResult, error) {
	return s.reconnectStorageMount(ctx, mountID, apply, "")
}

func (s *Service) reconnectStorageMount(ctx context.Context, mountID uint, apply bool, originalStorageID string) (StorageReconnectResult, error) {
	var mount models.StorageMount
	if err := s.Deps.DB.First(&mount, mountID).Error; err != nil {
		return StorageReconnectResult{}, err
	}
	if !mount.Mounted {
		return StorageReconnectResult{}, errors.New("storage mount is not mounted")
	}
	store, err := s.Deps.Storage.Store(mount.UUID)
	if err != nil {
		return StorageReconnectResult{}, err
	}
	var files []models.File
	filesQuery := s.Deps.DB.Select("id", "uuid").Where("storage_state = ?", models.FileStorageUnavailable)
	if originalStorageID != "" {
		filesQuery = filesQuery.Where("storage_id = ?", originalStorageID)
	}
	if err := filesQuery.Find(&files).Error; err != nil {
		return StorageReconnectResult{}, err
	}
	result := StorageReconnectResult{Scanned: len(files)}
	matchedIDs := make([]uint, 0)
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		prefix, err := s.Deps.Storage.Layout().FilePrefix(file.UUID)
		if err != nil {
			return result, err
		}
		walkErr := store.Walk(ctx, prefix, func(storage.ObjectInfo) error {
			return errStoragePrefixFound
		})
		if errors.Is(walkErr, errStoragePrefixFound) {
			matchedIDs = append(matchedIDs, file.ID)
			continue
		}
		if walkErr != nil {
			return result, walkErr
		}
	}
	result.Matched = len(matchedIDs)
	if !apply || len(matchedIDs) == 0 {
		return result, nil
	}
	for start := 0; start < len(matchedIDs); start += 500 {
		end := min(start+500, len(matchedIDs))
		update := s.Deps.DB.Model(&models.File{}).
			Where("id IN ? AND storage_state = ?", matchedIDs[start:end], models.FileStorageUnavailable).
			Updates(map[string]any{
				"storage_id":    mount.UUID,
				"storage_state": models.FileStorageAvailable,
			})
		if update.Error != nil {
			return result, update.Error
		}
		result.Relinked += int(update.RowsAffected)
	}
	return result, nil
}

func (s *Service) CreateStoragePool(input StoragePoolInput) (models.StoragePool, error) {
	pool := models.StoragePool{UUID: uuid.NewString(), Name: input.Name, IsDefault: input.IsDefault}
	err := s.saveStoragePool(&pool, input)
	return pool, err
}

func (s *Service) UpdateStoragePool(poolID uint, input StoragePoolInput) (models.StoragePool, error) {
	var pool models.StoragePool
	if err := s.Deps.DB.First(&pool, poolID).Error; err != nil {
		return models.StoragePool{}, err
	}
	if pool.System {
		if input.IsDefault && !pool.IsDefault {
			return pool, s.SetDefaultStoragePool(pool.ID)
		}
		return models.StoragePool{}, errors.New("built-in local pool cannot be edited")
	}
	err := s.saveStoragePool(&pool, input)
	return pool, err
}

func (s *Service) saveStoragePool(pool *models.StoragePool, input StoragePoolInput) error {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return errors.New("storage pool name is required")
	}
	mountIDs := uniqueUintValues(input.MountIDs)
	if len(mountIDs) == 0 {
		return errors.New("storage pool requires at least one mount")
	}
	var mountCount int64
	if err := s.Deps.DB.Model(&models.StorageMount{}).Where("id IN ?", mountIDs).Count(&mountCount).Error; err != nil {
		return err
	}
	if mountCount != int64(len(mountIDs)) {
		return errors.New("storage pool contains an unknown mount")
	}
	wasDefault := pool.IsDefault
	return s.Deps.DB.Transaction(func(tx *gorm.DB) error {
		if input.IsDefault {
			if err := tx.Model(&models.StoragePool{}).Where("is_default = ?", true).Update("is_default", false).Error; err != nil {
				return err
			}
		} else if wasDefault {
			var localPool models.StoragePool
			if err := tx.Where("uuid = ?", models.StoragePoolLocalUUID).First(&localPool).Error; err != nil {
				return fmt.Errorf("restore local default storage pool: %w", err)
			}
			if err := tx.Model(&localPool).Update("is_default", true).Error; err != nil {
				return err
			}
		}
		pool.Name = input.Name
		pool.IsDefault = input.IsDefault
		if err := tx.Save(pool).Error; err != nil {
			return err
		}
		if err := tx.Where("storage_pool_id = ?", pool.ID).Delete(&models.StoragePoolMount{}).Error; err != nil {
			return err
		}
		for _, mountID := range mountIDs {
			if err := tx.Create(&models.StoragePoolMount{StoragePoolID: pool.ID, StorageMountID: mountID}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) SetDefaultStoragePool(poolID uint) error {
	return s.Deps.DB.Transaction(func(tx *gorm.DB) error {
		var pool models.StoragePool
		if err := tx.First(&pool, poolID).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.StoragePool{}).Where("is_default = ?", true).Update("is_default", false).Error; err != nil {
			return err
		}
		return tx.Model(&pool).Update("is_default", true).Error
	})
}

func (s *Service) DeleteStoragePool(poolID uint) error {
	return s.Deps.DB.Transaction(func(tx *gorm.DB) error {
		var pool models.StoragePool
		if err := tx.First(&pool, poolID).Error; err != nil {
			return err
		}
		if pool.System {
			return errors.New("built-in local pool cannot be deleted")
		}
		if pool.IsDefault {
			var localPool models.StoragePool
			if err := tx.Where("uuid = ?", models.StoragePoolLocalUUID).First(&localPool).Error; err != nil {
				return err
			}
			if err := tx.Model(&localPool).Update("is_default", true).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&models.User{}).Where("storage_pool_id = ?", pool.ID).Update("storage_pool_id", nil).Error; err != nil {
			return err
		}
		if err := tx.Where("storage_pool_id = ?", pool.ID).Delete(&models.StoragePoolMount{}).Error; err != nil {
			return err
		}
		return tx.Delete(&pool).Error
	})
}

func checkStorageConnection(ctx context.Context, store storage.Store) error {
	checker, ok := store.(storage.HealthChecker)
	if !ok {
		return nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return checker.Check(checkCtx)
}

func uniqueUintValues(values []uint) []uint {
	seen := make(map[uint]bool, len(values))
	unique := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 || seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i] < unique[j] })
	return unique
}
