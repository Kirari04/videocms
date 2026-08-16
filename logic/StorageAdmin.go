package logic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
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

type StorageMountInput struct {
	Name          string
	Provider      string
	Configuration json.RawMessage
	Credentials   *json.RawMessage
}

type StorageMountResponse struct {
	ID                    uint
	UUID                  string
	Name                  string
	Provider              string
	Mounted               bool
	Available             bool
	System                bool
	Configuration         any `json:",omitempty"`
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
	UsedBytes            int64
	FileCount            int64
	UnavailableFileCount int64
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
	var totalUsedBytes int64
	var totalFileCount int64
	var totalUnavailableFileCount int64
	for _, usage := range usages {
		usageByMount[usage.StorageID] = usage
		totalUsedBytes += usage.UsedBytes
		totalFileCount += usage.FileCount
		totalUnavailableFileCount += usage.UnavailableFileCount
	}
	mountResponses := make([]StorageMountResponse, 0, len(mounts))
	for _, mount := range mounts {
		usage := usageByMount[mount.UUID]
		available := false
		if mount.Mounted && mount.LastError == "" && s.Deps.Storage != nil {
			_, runtimeErr := s.Deps.Storage.Store(mount.UUID)
			available = runtimeErr == nil
		}
		response := StorageMountResponse{
			ID:                    mount.ID,
			UUID:                  mount.UUID,
			Name:                  mount.Name,
			Provider:              mount.Provider,
			Mounted:               mount.Mounted,
			Available:             available,
			System:                mount.System,
			CredentialsConfigured: mount.EncryptedCredentials != "",
			UsedBytes:             usage.UsedBytes,
			FileCount:             usage.FileCount,
			UnavailableFileCount:  usage.UnavailableFileCount,
			LastError:             mount.LastError,
			LastCheckedAt:         mount.LastCheckedAt,
			UnmountedAt:           mount.UnmountedAt,
		}
		if !mount.System && mount.Configuration != "" {
			configuration, err := storage.DecodeMountConfiguration(mount.Provider, mount.Configuration)
			if err != nil {
				return StorageAdminOverview{}, fmt.Errorf("decode storage mount %s: %w", mount.UUID, err)
			}
			response.Configuration = configuration
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
		UsedBytes:            totalUsedBytes,
		FileCount:            totalFileCount,
		UnavailableFileCount: totalUnavailableFileCount,
		Mounts:               mountResponses,
		Pools:                poolResponses,
	}, nil
}

func (s *Service) CreateStorageMount(ctx context.Context, input StorageMountInput) (models.StorageMount, StorageReconnectResult, error) {
	if s.Deps.StorageCipher == nil {
		return models.StorageMount{}, StorageReconnectResult{}, storage.ErrEncryptionKeyNotConfigured
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return models.StorageMount{}, StorageReconnectResult{}, errors.New("storage mount name is required")
	}
	input.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	if input.Provider == "" {
		input.Provider = models.StorageProviderS3
	}
	if input.Provider != models.StorageProviderS3 && input.Provider != models.StorageProviderSFTP {
		return models.StorageMount{}, StorageReconnectResult{}, fmt.Errorf("unsupported storage provider %q", input.Provider)
	}
	mount := models.StorageMount{
		UUID:     uuid.NewString(),
		Name:     input.Name,
		Provider: input.Provider,
		Mounted:  true,
	}
	configuration, credentials, err := storage.EncodeMount(mount.Provider, input.Configuration, input.Credentials, "", mount.UUID, s.Deps.StorageCipher)
	if err != nil {
		return models.StorageMount{}, StorageReconnectResult{}, err
	}
	mount.Configuration = configuration
	mount.EncryptedCredentials = credentials
	store, err := storage.NewStoreFromMount(ctx, mount.Provider, mount.UUID, configuration, credentials, s.Deps.StorageCipher)
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
	if err := s.Deps.Storage.RegisterStore(mount.UUID, store); err != nil {
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

func (s *Service) UpdateStorageMount(ctx context.Context, mountID uint, input StorageMountInput) (models.StorageMount, error) {
	if s.Deps.StorageCipher == nil {
		return models.StorageMount{}, storage.ErrEncryptionKeyNotConfigured
	}
	var mount models.StorageMount
	if err := s.Deps.DB.First(&mount, mountID).Error; err != nil {
		return models.StorageMount{}, err
	}
	if mount.System {
		return models.StorageMount{}, errors.New("storage mount cannot be edited")
	}
	if err := s.ensureStorageMountNotMigrating(mount.UUID); err != nil {
		return models.StorageMount{}, err
	}
	releaseMount := s.Deps.StorageLifecycle.WriteLock(mount.UUID)
	defer releaseMount()
	if err := s.Deps.DB.First(&mount, mountID).Error; err != nil {
		return models.StorageMount{}, err
	}
	if err := s.ensureStorageMountNotMigrating(mount.UUID); err != nil {
		return models.StorageMount{}, err
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return models.StorageMount{}, errors.New("storage mount name is required")
	}
	input.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	if input.Provider == "" {
		input.Provider = mount.Provider
	}
	if input.Provider != mount.Provider {
		return models.StorageMount{}, errors.New("storage mount provider cannot be changed")
	}
	configuration, encryptedCredentials, err := storage.EncodeMount(mount.Provider, input.Configuration, input.Credentials, mount.EncryptedCredentials, mount.UUID, s.Deps.StorageCipher)
	if err != nil {
		return models.StorageMount{}, err
	}
	if mount.Mounted {
		sameLocation, locationErr := storage.SameMountLocation(mount.Provider, mount.Configuration, configuration)
		if locationErr != nil {
			return models.StorageMount{}, fmt.Errorf("compare storage mount location: %w", locationErr)
		}
		if !sameLocation {
			return models.StorageMount{}, errors.New("detach the storage mount before changing its remote storage location")
		}
	}
	store, err := storage.NewStoreFromMount(ctx, mount.Provider, mount.UUID, configuration, encryptedCredentials, s.Deps.StorageCipher)
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
		if err := s.Deps.Storage.RegisterStore(mount.UUID, store); err != nil {
			_ = store.Close()
			return models.StorageMount{}, err
		}
	} else {
		_ = store.Close()
	}
	return mount, nil
}

// CreateS3StorageMount is retained for internal callers while the public
// storage API is provider-neutral.
func (s *Service) CreateS3StorageMount(ctx context.Context, input S3StorageMountInput) (models.StorageMount, StorageReconnectResult, error) {
	configuration, err := json.Marshal(input.Configuration)
	if err != nil {
		return models.StorageMount{}, StorageReconnectResult{}, err
	}
	var credentials *json.RawMessage
	if input.Credentials != nil {
		encoded, err := json.Marshal(input.Credentials)
		if err != nil {
			return models.StorageMount{}, StorageReconnectResult{}, err
		}
		raw := json.RawMessage(encoded)
		credentials = &raw
	}
	return s.CreateStorageMount(ctx, StorageMountInput{
		Name: input.Name, Provider: models.StorageProviderS3,
		Configuration: configuration, Credentials: credentials,
	})
}

func (s *Service) UpdateS3StorageMount(ctx context.Context, mountID uint, input S3StorageMountInput) (models.StorageMount, error) {
	configuration, err := json.Marshal(input.Configuration)
	if err != nil {
		return models.StorageMount{}, err
	}
	var credentials *json.RawMessage
	if input.Credentials != nil {
		encoded, err := json.Marshal(input.Credentials)
		if err != nil {
			return models.StorageMount{}, err
		}
		raw := json.RawMessage(encoded)
		credentials = &raw
	}
	return s.UpdateStorageMount(ctx, mountID, StorageMountInput{
		Name: input.Name, Provider: models.StorageProviderS3,
		Configuration: configuration, Credentials: credentials,
	})
}

func (s *Service) UnmountStorageMount(mountID uint) (int64, error) {
	var mount models.StorageMount
	if err := s.Deps.DB.First(&mount, mountID).Error; err != nil {
		return 0, err
	}
	if mount.System {
		return 0, errors.New("built-in local storage cannot be unmounted")
	}
	if err := s.ensureStorageMountNotMigrating(mount.UUID); err != nil {
		return 0, err
	}
	releaseMount := s.Deps.StorageLifecycle.WriteLock(mount.UUID)
	defer releaseMount()
	if err := s.Deps.DB.First(&mount, mountID).Error; err != nil {
		return 0, err
	}
	if err := s.ensureStorageMountNotMigrating(mount.UUID); err != nil {
		return 0, err
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
	if err := s.Deps.Storage.UnregisterStore(mount.UUID); err != nil && !errors.Is(err, storage.ErrStoreNotConfigured) {
		return unavailable, err
	}
	return unavailable, nil
}

// DeleteStorageMount permanently removes a detached mount's saved
// configuration. File records retain the old storage UUID and stay
// unavailable so an administrator can reconnect them from a replacement
// mount without keeping credentials for storage that is no longer used.
func (s *Service) DeleteStorageMount(mountID uint) (int64, error) {
	var mount models.StorageMount
	if err := s.Deps.DB.First(&mount, mountID).Error; err != nil {
		return 0, err
	}
	if mount.System {
		return 0, errors.New("built-in local storage cannot be deleted")
	}
	if err := s.ensureStorageMountNotMigrating(mount.UUID); err != nil {
		return 0, err
	}

	releaseMount := s.Deps.StorageLifecycle.WriteLock(mount.UUID)
	defer releaseMount()
	if err := s.Deps.DB.First(&mount, mountID).Error; err != nil {
		return 0, err
	}
	if err := s.ensureStorageMountNotMigrating(mount.UUID); err != nil {
		return 0, err
	}
	if mount.Mounted {
		return 0, errors.New("detach the storage mount before deleting it")
	}

	var unavailable int64
	err := s.Deps.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.File{}).
			Where("storage_id = ? AND storage_state <> ?", mount.UUID, models.FileStorageUnavailable).
			Update("storage_state", models.FileStorageUnavailable).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.File{}).
			Where("storage_id = ? AND storage_state = ?", mount.UUID, models.FileStorageUnavailable).
			Count(&unavailable).Error; err != nil {
			return err
		}
		if err := tx.Where("storage_mount_id = ?", mount.ID).Delete(&models.StoragePoolMount{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Delete(&mount).Error
	})
	return unavailable, err
}

func (s *Service) RemountStorageMount(ctx context.Context, mountID uint) (StorageReconnectResult, error) {
	if s.Deps.StorageCipher == nil {
		return StorageReconnectResult{}, storage.ErrEncryptionKeyNotConfigured
	}
	var mount models.StorageMount
	if err := s.Deps.DB.First(&mount, mountID).Error; err != nil {
		return StorageReconnectResult{}, err
	}
	if mount.System {
		return StorageReconnectResult{}, errors.New("storage mount cannot be remounted")
	}
	releaseMount := s.Deps.StorageLifecycle.WriteLock(mount.UUID)
	if err := s.Deps.DB.First(&mount, mountID).Error; err != nil {
		releaseMount()
		return StorageReconnectResult{}, err
	}
	if mount.Mounted {
		releaseMount()
		return StorageReconnectResult{}, errors.New("storage mount is already mounted")
	}
	store, err := storage.NewStoreFromMount(ctx, mount.Provider, mount.UUID, mount.Configuration, mount.EncryptedCredentials, s.Deps.StorageCipher)
	if err != nil {
		releaseMount()
		return StorageReconnectResult{}, err
	}
	if err := checkStorageConnection(ctx, store); err != nil {
		_ = store.Close()
		releaseMount()
		return StorageReconnectResult{}, err
	}
	if err := s.Deps.Storage.RegisterStore(mount.UUID, store); err != nil {
		_ = store.Close()
		releaseMount()
		return StorageReconnectResult{}, err
	}
	now := time.Now().UTC()
	if err := s.Deps.DB.Model(&mount).Updates(map[string]any{
		"mounted":         true,
		"unmounted_at":    nil,
		"last_error":      "",
		"last_checked_at": &now,
	}).Error; err != nil {
		_ = s.Deps.Storage.UnregisterStore(mount.UUID)
		releaseMount()
		return StorageReconnectResult{}, err
	}
	releaseMount()
	reconnect, reconnectErr := s.ReconnectStorageMountFiles(ctx, mount.ID, true, mount.UUID)
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
	releaseMount := s.Deps.StorageLifecycle.ReadLock(mount.UUID)
	defer releaseMount()
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

const storageReconnectConcurrency = 8
const storageReconnectFileBatchSize = 100

func (s *Service) ReconnectStorageMount(ctx context.Context, mountID uint, apply bool) (StorageReconnectResult, error) {
	return s.ReconnectStorageMountFiles(ctx, mountID, apply, "")
}

// ReconnectStorageMountFiles scans one mounted backend for unavailable files.
// originalStorageID limits startup/remount recovery to records that already
// belonged to that mount; an empty value enables an administrator-requested
// migration scan across all unavailable records.
func (s *Service) ReconnectStorageMountFiles(ctx context.Context, mountID uint, apply bool, originalStorageID string) (StorageReconnectResult, error) {
	var mount models.StorageMount
	if err := s.Deps.DB.First(&mount, mountID).Error; err != nil {
		return StorageReconnectResult{}, err
	}
	releaseMount := s.Deps.StorageLifecycle.ReadLock(mount.UUID)
	if err := s.Deps.DB.First(&mount, mountID).Error; err != nil {
		releaseMount()
		return StorageReconnectResult{}, err
	}
	if !mount.Mounted {
		releaseMount()
		return StorageReconnectResult{}, errors.New("storage mount is not mounted")
	}
	if _, err := s.Deps.Storage.Store(mount.UUID); err != nil {
		releaseMount()
		return StorageReconnectResult{}, err
	}
	releaseMount()

	result := StorageReconnectResult{}
	lastFileID := uint(0)
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		var files []models.File
		filesQuery := s.Deps.DB.
			Preload("Qualitys").
			Preload("Audios").
			Preload("Subtitles").
			Where("storage_state = ? AND id > ?", models.FileStorageUnavailable, lastFileID).
			Order("id ASC").
			Limit(storageReconnectFileBatchSize)
		if originalStorageID != "" {
			filesQuery = filesQuery.Where("storage_id = ?", originalStorageID)
		}
		if err := filesQuery.Find(&files).Error; err != nil {
			return result, err
		}
		if len(files) == 0 {
			return result, nil
		}
		lastFileID = files[len(files)-1].ID
		if err := s.reconnectStorageFileBatch(ctx, mount, files, apply, &result); err != nil {
			return result, err
		}
	}
}

func (s *Service) reconnectStorageFileBatch(ctx context.Context, mount models.StorageMount, files []models.File, apply bool, result *StorageReconnectResult) error {
	releaseMount := s.Deps.StorageLifecycle.ReadLock(mount.UUID)
	defer releaseMount()
	var currentMount models.StorageMount
	if err := s.Deps.DB.First(&currentMount, mount.ID).Error; err != nil {
		return err
	}
	if !currentMount.Mounted {
		return errors.New("storage mount was detached during reconnect scan")
	}
	store, err := s.Deps.Storage.Store(mount.UUID)
	if err != nil {
		return err
	}
	type scanResult struct {
		fileID  uint
		matched bool
		err     error
	}
	scanCtx, cancelScan := context.WithCancel(ctx)
	defer cancelScan()
	jobs := make(chan models.File)
	results := make(chan scanResult)
	workerCount := min(storageReconnectConcurrency, len(files))
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for file := range jobs {
				matched, scanErr := s.storageFileMatches(scanCtx, store, file)
				select {
				case results <- scanResult{fileID: file.ID, matched: matched, err: scanErr}:
				case <-scanCtx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, file := range files {
			select {
			case jobs <- file:
			case <-scanCtx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	matchedIDs := make([]uint, 0, 100)
	flushMatches := func() error {
		if !apply || len(matchedIDs) == 0 {
			matchedIDs = matchedIDs[:0]
			return nil
		}
		update := s.Deps.DB.Model(&models.File{}).
			Where("id IN ? AND storage_state = ?", matchedIDs, models.FileStorageUnavailable).
			Updates(map[string]any{
				"storage_id":    mount.UUID,
				"storage_state": models.FileStorageAvailable,
			})
		if update.Error != nil {
			return update.Error
		}
		result.Relinked += int(update.RowsAffected)
		matchedIDs = matchedIDs[:0]
		return nil
	}
	var scanErr error
	for scanned := range results {
		result.Scanned++
		if scanned.err != nil {
			if scanErr == nil {
				scanErr = scanned.err
				cancelScan()
			}
			continue
		}
		if !scanned.matched {
			continue
		}
		result.Matched++
		matchedIDs = append(matchedIDs, scanned.fileID)
		if len(matchedIDs) == cap(matchedIDs) {
			if err := flushMatches(); err != nil && scanErr == nil {
				scanErr = err
				cancelScan()
			}
		}
	}
	if err := flushMatches(); err != nil && scanErr == nil {
		scanErr = err
	}
	if scanErr != nil {
		return scanErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (s *Service) storageFileMatches(ctx context.Context, store storage.Store, file models.File) (bool, error) {
	anchors, err := s.storageReconnectAnchors(file)
	if err != nil {
		return false, err
	}
	if len(anchors) > 0 {
		for _, anchor := range anchors {
			if _, err := store.Stat(ctx, anchor); err != nil {
				if errors.Is(err, storage.ErrNotFound) {
					return false, nil
				}
				return false, err
			}
		}
		return true, nil
	}
	prefix, err := s.Deps.Storage.Layout().FilePrefix(file.UUID)
	if err != nil {
		return false, err
	}
	walkErr := store.Walk(ctx, prefix, func(storage.ObjectInfo) error {
		return errStoragePrefixFound
	})
	if errors.Is(walkErr, errStoragePrefixFound) {
		return true, nil
	}
	return false, walkErr
}

func (s *Service) storageReconnectAnchors(file models.File) ([]storage.Key, error) {
	anchors := make([]storage.Key, 0, 1+len(file.Qualitys)+len(file.Audios)+len(file.Subtitles))
	if file.SourceKey != "" {
		key, err := storage.ParseKey(file.SourceKey)
		if err != nil {
			return nil, err
		}
		anchors = append(anchors, key)
	}
	for _, quality := range file.Qualitys {
		if !quality.Ready || quality.OutputFile == "" {
			continue
		}
		key, err := s.Deps.Storage.Layout().Video(file.UUID, quality.Name, quality.OutputFile)
		if err != nil {
			return nil, err
		}
		anchors = append(anchors, key)
	}
	for _, audio := range file.Audios {
		if !audio.Ready || audio.OutputFile == "" {
			continue
		}
		key, err := s.Deps.Storage.Layout().Audio(file.UUID, audio.UUID, audio.OutputFile)
		if err != nil {
			return nil, err
		}
		anchors = append(anchors, key)
	}
	for _, subtitle := range file.Subtitles {
		if !subtitle.Ready || subtitle.OutputFile == "" {
			continue
		}
		key, err := s.Deps.Storage.Layout().Subtitle(file.UUID, subtitle.UUID, subtitle.OutputFile)
		if err != nil {
			return nil, err
		}
		anchors = append(anchors, key)
	}
	return anchors, nil
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
	var releasePool func()
	if pool.ID > 0 {
		releasePool = s.Deps.StorageLifecycle.PoolWriteLock(pool.ID)
		defer releasePool()
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return errors.New("storage pool name is required")
	}
	if pool.ID > 0 {
		if err := s.ensureStoragePoolNotMigrating(pool.ID); err != nil {
			return err
		}
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
	releasePool := s.Deps.StorageLifecycle.PoolWriteLock(poolID)
	defer releasePool()
	if err := s.ensureStoragePoolNotMigrating(poolID); err != nil {
		return err
	}
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
