package mediacache

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"ch/kirari04/videocms/background"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	MinimumFreePercent  = 10
	recoveryFreePercent = 12
	cacheHighWatermark  = 0.90
	cacheLowWatermark   = 0.80
	promotionTimeout    = 30 * time.Minute
)

var errCacheAdmissionSkipped = errors.New("cache admission skipped")
var errCacheCaptureStale = fmt.Errorf("captured file changed: %w", errCacheAdmissionSkipped)

func AdmissionSkipped(err error) bool {
	return errors.Is(err, errCacheAdmissionSkipped)
}

type OpenRequest struct {
	PoolID        uint
	OriginMountID string
	FileID        uint
	Key           storage.Key
}

// OpenResult identifies the mount that actually served the response. On a
// miss this is the authoritative mount; on a hit it is the cache mount.
type OpenResult struct {
	CacheHit    bool
	CacheStatus string
	PoolID      uint
	MountUUID   string
}

const (
	CacheStatusBypass  = "BYPASS"
	CacheStatusHit     = "HIT"
	CacheStatusMiss    = "MISS"
	CacheStatusFilling = "FILLING"
)

type PromotionPayload struct {
	PoolID           uint               `json:"poolId"`
	OriginMountID    string             `json:"originMountId"`
	FileID           uint               `json:"fileId"`
	FileCacheVersion uint64             `json:"fileCacheVersion"`
	ObjectKey        string             `json:"objectKey"`
	TargetMountID    uint               `json:"targetMountId"`
	TargetMountIDs   []uint             `json:"targetMountIds,omitempty"`
	TemporaryPath    string             `json:"temporaryPath"`
	Info             storage.ObjectInfo `json:"info"`
}

type MembershipStats struct {
	MountID        uint
	MaxBytes       int64
	UsedBytes      int64
	EntryCount     int64
	CapacityKnown  bool
	CapacityTotal  uint64
	CapacityFree   uint64
	FreePercent    float64
	MinimumFreePct int
}

type target struct {
	MountID   uint
	MountUUID string
	MaxBytes  int64
	UsedBytes int64
}

type Service struct {
	db      *gorm.DB
	storage *storage.Service
	runtime *background.Runtime
	logger  *log.Logger

	mu                     sync.Mutex
	active                 map[string]struct{}
	closed                 bool
	workers                sync.WaitGroup
	reservedWorkspaceBytes int64

	membershipMu    sync.Mutex
	membershipLocks map[string]*sync.Mutex
}

func New(db *gorm.DB, stores *storage.Service, runtime *background.Runtime) *Service {
	return &Service{
		db: db, storage: stores, runtime: runtime, logger: log.Default(), active: make(map[string]struct{}),
		membershipLocks: make(map[string]*sync.Mutex),
	}
}

func (s *Service) Open(ctx context.Context, request OpenRequest) (*storage.Object, bool, error) {
	object, result, err := s.OpenWithResult(ctx, request)
	return object, result.CacheHit, err
}

func (s *Service) OpenWithResult(ctx context.Context, request OpenRequest) (*storage.Object, OpenResult, error) {
	result := OpenResult{CacheStatus: CacheStatusBypass, PoolID: request.PoolID, MountUUID: request.OriginMountID}
	if s == nil || s.db == nil || s.storage == nil || request.OriginMountID == "" || request.Key.IsZero() {
		return nil, result, storage.ErrStoreNotConfigured
	}
	poolID := request.PoolID
	if poolID == 0 {
		poolID = s.resolvePoolID(ctx, request.OriginMountID)
	}
	result.PoolID = poolID
	if poolID != 0 {
		if object, mountUUID, ok := s.openCached(ctx, poolID, request); ok {
			result.CacheHit = true
			result.CacheStatus = CacheStatusHit
			result.MountUUID = mountUUID
			return object, result, nil
		}
	}

	origin, err := s.storage.StoreOrDefault(request.OriginMountID)
	if err != nil {
		return nil, result, err
	}
	object, err := origin.Open(ctx, request.Key)
	if err != nil {
		return nil, result, err
	}
	if poolID == 0 || request.FileID == 0 || object.Info.Size <= 0 {
		return object, result, nil
	}
	fileCacheVersion, err := s.fileCacheVersion(ctx, request.FileID)
	if err != nil {
		return object, result, nil
	}
	targets, err := s.targets(ctx, poolID, request.OriginMountID, object.Info.Size)
	if err != nil || len(targets) == 0 {
		if s.cacheConfigured(ctx, poolID) {
			result.CacheStatus = CacheStatusMiss
		}
		if err != nil {
			s.logger.Printf("component=storage_cache event=target_resolution_failed pool=%d error=%q", poolID, err)
		}
		return object, result, nil
	}
	claimKey := cacheIdentity(poolID, request.OriginMountID, request.Key.String())
	if !s.claim(claimKey) {
		result.CacheStatus = CacheStatusFilling
		return object, result, nil
	}
	selected := targets[0]
	payload := PromotionPayload{
		PoolID: poolID, OriginMountID: request.OriginMountID, FileID: request.FileID,
		FileCacheVersion: fileCacheVersion, ObjectKey: request.Key.String(), TargetMountID: selected.MountID,
		TargetMountIDs: targetMountIDs(targets), Info: object.Info,
	}
	result.CacheStatus = CacheStatusFilling
	releaseWorkspace, ok := s.reserveWorkspace(ctx, object.Info.Size)
	if !ok {
		s.fillWithoutResponseCapture(claimKey, payload)
		return object, result, nil
	}
	temporary, cleanup, err := s.storage.Workspace().TempFile(ctx, "playback-cache", "")
	if err != nil {
		releaseWorkspace()
		s.fillWithoutResponseCapture(claimKey, payload)
		return object, result, nil
	}
	payload.TemporaryPath = temporary.Name()
	object.Body = newCaptureBody(object.Body, temporary, object.Info.Size, func(outcome captureOutcome) {
		releaseWorkspace()
		if !outcome.Started {
			_ = cleanup()
			s.release(claimKey)
			return
		}
		if !outcome.Complete {
			// Browsers are allowed to cancel, seek, or range-read media. That must
			// not make an on-demand cache permanently ineffective. Discard the
			// partial capture and finish the requested object independently of the
			// client connection.
			_ = cleanup()
			payload.TemporaryPath = ""
			if s.enqueuePromotion(payload, "Fill requested playback data", background.VisibilitySystem, errors.New("client response did not consume the complete object")) {
				s.release(claimKey)
				return
			}
			s.promoteAsync(claimKey, payload, func() error { return nil })
			return
		}
		s.promoteAsync(claimKey, payload, cleanup)
	})
	return object, result, nil
}

func (s *Service) cacheConfigured(ctx context.Context, poolID uint) bool {
	if poolID == 0 {
		return false
	}
	var count int64
	err := s.db.WithContext(ctx).Model(&models.StoragePoolMount{}).
		Where("storage_pool_id = ? AND role = ?", poolID, models.StoragePoolMountCache).
		Limit(1).Count(&count).Error
	return err == nil && count > 0
}

func (s *Service) fillWithoutResponseCapture(claimKey string, payload PromotionPayload) {
	payload.TemporaryPath = ""
	if s.enqueuePromotion(payload, "Fill requested playback data", background.VisibilitySystem, errors.New("response capture workspace is unavailable")) {
		s.release(claimKey)
		return
	}
	s.promoteAsync(claimKey, payload, func() error { return nil })
}

// Fill completes a cache promotion. A payload with TemporaryPath set promotes
// a completed response capture. Without one, it performs a fresh on-demand
// origin read so interrupted and range-based playback can still populate the
// cache without depending on the lifetime of the HTTP request.
func (s *Service) Fill(ctx context.Context, payload PromotionPayload) error {
	if payload.TemporaryPath != "" {
		return s.Promote(ctx, payload)
	}
	return s.captureFromOrigin(ctx, payload)
}

func (s *Service) captureFromOrigin(ctx context.Context, payload PromotionPayload) error {
	if payload.OriginMountID == "" || payload.ObjectKey == "" || payload.FileID == 0 || payload.Info.Size <= 0 {
		return errors.New("cache origin capture payload is incomplete")
	}
	key, err := storage.ParseKey(payload.ObjectKey)
	if err != nil {
		return err
	}
	if object, _, ok := s.openCached(ctx, payload.PoolID, OpenRequest{
		PoolID: payload.PoolID, OriginMountID: payload.OriginMountID, FileID: payload.FileID, Key: key,
	}); ok {
		_ = object.Body.Close()
		return nil
	}
	version, err := s.fileCacheVersion(ctx, payload.FileID)
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && version != payload.FileCacheVersion) {
		return errCacheAdmissionSkipped
	}
	if err != nil {
		return err
	}
	var combined error
	for _, targetMountID := range uniqueTargetMountIDs(payload) {
		if err := ctx.Err(); err != nil {
			return errors.Join(combined, err)
		}
		err = s.promoteOriginToTarget(ctx, payload, key, targetMountID)
		if err == nil {
			return nil
		}
		if errors.Is(err, errCacheCaptureStale) {
			return errCacheAdmissionSkipped
		}
		if AdmissionSkipped(err) {
			continue
		}
		s.recordMembershipError(payload.PoolID, targetMountID, err)
		combined = errors.Join(combined, fmt.Errorf("cache mount %d: %w", targetMountID, err))
	}
	if combined != nil {
		return combined
	}
	return errCacheAdmissionSkipped
}

func (s *Service) promoteOriginToTarget(ctx context.Context, payload PromotionPayload, objectKey storage.Key, targetMountID uint) error {
	unlock := s.lockMembership(payload.PoolID, targetMountID)
	defer unlock()
	var membership models.StoragePoolMount
	if err := s.db.WithContext(ctx).
		Where("storage_pool_id = ? AND storage_mount_id = ? AND role = ?", payload.PoolID, targetMountID, models.StoragePoolMountCache).
		First(&membership).Error; err != nil {
		return errCacheAdmissionSkipped
	}
	var mount models.StorageMount
	if err := s.db.WithContext(ctx).First(&mount, targetMountID).Error; err != nil {
		return err
	}
	if !mount.Mounted || mount.LastError != "" || mount.UUID == payload.OriginMountID {
		return errCacheAdmissionSkipped
	}
	target, err := s.storage.Store(mount.UUID)
	if err != nil {
		return err
	}
	if err := s.pruneMembership(ctx, membership, target, payload.Info.Size); err != nil {
		return err
	}
	if membership.CacheMaxBytes <= 0 || payload.Info.Size > membership.CacheMaxBytes {
		return errCacheAdmissionSkipped
	}
	origin, err := s.storage.StoreOrDefault(payload.OriginMountID)
	if err != nil {
		return err
	}
	object, err := origin.Open(ctx, objectKey)
	if err != nil {
		return err
	}
	if object.Info.Size != payload.Info.Size {
		_ = object.Body.Close()
		return fmt.Errorf("cache origin size changed from %d to %d: %w", payload.Info.Size, object.Info.Size, errCacheAdmissionSkipped)
	}
	if payload.Info.ETag != "" && object.Info.ETag != "" && payload.Info.ETag != object.Info.ETag {
		_ = object.Body.Close()
		return fmt.Errorf("cache origin entity changed: %w", errCacheAdmissionSkipped)
	}
	cacheKey, err := s.cacheKey(payload.PoolID, payload.OriginMountID, payload.ObjectKey)
	if err != nil {
		_ = object.Body.Close()
		return err
	}
	expected := object.Info.Size
	written, putErr := target.Put(ctx, cacheKey, object.Body, storage.PutOptions{
		ExpectedSize: &expected, ContentType: object.Info.ContentType, CacheControl: object.Info.CacheControl,
	})
	closeErr := object.Body.Close()
	if putErr != nil || closeErr != nil {
		_ = target.Delete(context.WithoutCancel(ctx), cacheKey)
		return errors.Join(putErr, closeErr)
	}
	if written.Size != expected {
		_ = target.Delete(context.WithoutCancel(ctx), cacheKey)
		return fmt.Errorf("cache object size mismatch: wrote %d, expected %d", written.Size, expected)
	}
	now := time.Now().UTC()
	modTime := object.Info.ModTime
	entry := models.StorageCacheEntry{
		StoragePoolID: payload.PoolID, OriginMountID: payload.OriginMountID,
		ObjectKeyHash: objectKeyHash(payload.ObjectKey), ObjectKey: payload.ObjectKey,
		CacheMountID: targetMountID, CacheObjectKey: cacheKey.String(), FileID: payload.FileID,
		FileCacheVersion: payload.FileCacheVersion,
		Size:             expected, SourceETag: object.Info.ETag, ContentType: object.Info.ContentType,
		CacheControl: object.Info.CacheControl, SourceModTime: &modTime, LastAccessedAt: now,
	}
	if err := s.commitPromotion(context.WithoutCancel(ctx), &entry); err != nil {
		_ = target.Delete(context.WithoutCancel(ctx), cacheKey)
		return err
	}
	s.clearMembershipError(payload.PoolID, targetMountID)
	return nil
}

func (s *Service) openCached(ctx context.Context, poolID uint, request OpenRequest) (*storage.Object, string, bool) {
	hash := objectKeyHash(request.Key.String())
	var entry models.StorageCacheEntry
	err := s.db.WithContext(ctx).
		Joins("JOIN storage_pool_mounts AS cache_membership ON cache_membership.storage_pool_id = storage_cache_entries.storage_pool_id AND cache_membership.storage_mount_id = storage_cache_entries.cache_mount_id AND cache_membership.role = ?", models.StoragePoolMountCache).
		Joins("JOIN files AS cache_file ON cache_file.id = storage_cache_entries.file_id AND cache_file.storage_cache_version = storage_cache_entries.file_cache_version AND cache_file.deleted_at IS NULL").
		Where("storage_cache_entries.storage_pool_id = ? AND storage_cache_entries.origin_mount_id = ? AND storage_cache_entries.object_key_hash = ?", poolID, request.OriginMountID, hash).
		First(&entry).Error
	if err != nil {
		return nil, "", false
	}
	var mount models.StorageMount
	if err := s.db.WithContext(ctx).First(&mount, entry.CacheMountID).Error; err != nil || !mount.Mounted || mount.LastError != "" {
		return nil, "", false
	}
	store, err := s.storage.Store(mount.UUID)
	if err != nil {
		return nil, "", false
	}
	key, err := storage.ParseKey(entry.CacheObjectKey)
	if err != nil {
		s.dropEntry(context.WithoutCancel(ctx), entry, false)
		return nil, "", false
	}
	object, err := store.Open(ctx, key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			s.dropEntry(context.WithoutCancel(ctx), entry, false)
		}
		return nil, "", false
	}
	if object.Info.Size != entry.Size {
		_ = object.Body.Close()
		s.dropEntry(context.WithoutCancel(ctx), entry, true)
		return nil, "", false
	}
	object.Info.Key = request.Key
	object.Info.Size = entry.Size
	object.Info.ETag = entry.SourceETag
	object.Info.ContentType = entry.ContentType
	object.Info.CacheControl = entry.CacheControl
	if entry.SourceModTime != nil {
		object.Info.ModTime = *entry.SourceModTime
	}
	now := time.Now().UTC()
	_ = s.db.WithContext(context.WithoutCancel(ctx)).Model(&models.StorageCacheEntry{}).
		Where("id = ? AND last_accessed_at < ?", entry.ID, now.Add(-5*time.Minute)).
		Update("last_accessed_at", now).Error
	return object, mount.UUID, true
}

func (s *Service) promoteAsync(claimKey string, payload PromotionPayload, cleanup func() error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = cleanup()
		s.release(claimKey)
		return
	}
	s.workers.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.workers.Done()
		defer s.release(claimKey)
		ctx, cancel := context.WithTimeout(context.Background(), promotionTimeout)
		defer cancel()
		err := s.Fill(ctx, payload)
		if err == nil || errors.Is(err, errCacheAdmissionSkipped) {
			_ = cleanup()
			return
		}
		if s.enqueuePromotionRetry(payload, err) {
			return
		}
		_ = cleanup()
		s.recordMembershipError(payload.PoolID, payload.TargetMountID, err)
		s.logger.Printf("component=storage_cache event=promotion_failed pool=%d mount=%d key=%q error=%q", payload.PoolID, payload.TargetMountID, payload.ObjectKey, err)
	}()
}

func (s *Service) Promote(ctx context.Context, payload PromotionPayload) error {
	targetIDs := uniqueTargetMountIDs(payload)
	if payload.TemporaryPath == "" || payload.ObjectKey == "" || len(targetIDs) == 0 || payload.Info.Size <= 0 {
		return errors.New("cache promotion payload is incomplete")
	}
	version, err := s.fileCacheVersion(ctx, payload.FileID)
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && version != payload.FileCacheVersion) {
		return errCacheAdmissionSkipped
	}
	if err != nil {
		return err
	}
	var combined error
	for _, targetMountID := range targetIDs {
		if err := ctx.Err(); err != nil {
			return errors.Join(combined, err)
		}
		err = s.promoteToTarget(ctx, payload, targetMountID)
		if err == nil {
			return nil
		}
		if errors.Is(err, errCacheCaptureStale) {
			return errCacheAdmissionSkipped
		}
		if AdmissionSkipped(err) {
			continue
		}
		s.recordMembershipError(payload.PoolID, targetMountID, err)
		combined = errors.Join(combined, fmt.Errorf("cache mount %d: %w", targetMountID, err))
	}
	if combined != nil {
		return combined
	}
	return errCacheAdmissionSkipped
}

func (s *Service) promoteToTarget(ctx context.Context, payload PromotionPayload, targetMountID uint) error {
	unlock := s.lockMembership(payload.PoolID, targetMountID)
	defer unlock()
	var membership models.StoragePoolMount
	if err := s.db.WithContext(ctx).
		Where("storage_pool_id = ? AND storage_mount_id = ? AND role = ?", payload.PoolID, targetMountID, models.StoragePoolMountCache).
		First(&membership).Error; err != nil {
		return errCacheAdmissionSkipped
	}
	var mount models.StorageMount
	if err := s.db.WithContext(ctx).First(&mount, targetMountID).Error; err != nil {
		return err
	}
	if !mount.Mounted || mount.LastError != "" || mount.UUID == payload.OriginMountID {
		return errCacheAdmissionSkipped
	}
	store, err := s.storage.Store(mount.UUID)
	if err != nil {
		return err
	}
	if err := s.pruneMembership(ctx, membership, store, payload.Info.Size); err != nil {
		return err
	}
	if membership.CacheMaxBytes <= 0 || payload.Info.Size > membership.CacheMaxBytes {
		return errCacheAdmissionSkipped
	}
	file, err := os.Open(payload.TemporaryPath)
	if err != nil {
		return err
	}
	defer file.Close()
	cacheKey, err := s.cacheKey(payload.PoolID, payload.OriginMountID, payload.ObjectKey)
	if err != nil {
		return err
	}
	expected := payload.Info.Size
	written, err := store.Put(ctx, cacheKey, file, storage.PutOptions{
		ExpectedSize: &expected, ContentType: payload.Info.ContentType, CacheControl: payload.Info.CacheControl,
	})
	if err != nil {
		_ = store.Delete(context.WithoutCancel(ctx), cacheKey)
		return err
	}
	if written.Size != expected {
		_ = store.Delete(context.WithoutCancel(ctx), cacheKey)
		return fmt.Errorf("cache object size mismatch: wrote %d, expected %d", written.Size, expected)
	}
	now := time.Now().UTC()
	modTime := payload.Info.ModTime
	entry := models.StorageCacheEntry{
		StoragePoolID: payload.PoolID, OriginMountID: payload.OriginMountID,
		ObjectKeyHash: objectKeyHash(payload.ObjectKey), ObjectKey: payload.ObjectKey,
		CacheMountID: targetMountID, CacheObjectKey: cacheKey.String(), FileID: payload.FileID,
		FileCacheVersion: payload.FileCacheVersion,
		Size:             expected, SourceETag: payload.Info.ETag, ContentType: payload.Info.ContentType,
		CacheControl: payload.Info.CacheControl, SourceModTime: &modTime, LastAccessedAt: now,
	}
	if err := s.commitPromotion(context.WithoutCancel(ctx), &entry); err != nil {
		_ = store.Delete(context.WithoutCancel(ctx), cacheKey)
		return err
	}
	s.clearMembershipError(payload.PoolID, targetMountID)
	return nil
}

func uniqueTargetMountIDs(payload PromotionPayload) []uint {
	values := append([]uint(nil), payload.TargetMountIDs...)
	if len(values) == 0 {
		values = append(values, payload.TargetMountID)
	}
	seen := make(map[uint]bool, len(values))
	result := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func targetMountIDs(targets []target) []uint {
	result := make([]uint, 0, len(targets))
	for _, target := range targets {
		result = append(result, target.MountID)
	}
	return result
}

func (s *Service) enqueuePromotionRetry(payload PromotionPayload, cause error) bool {
	return s.enqueuePromotion(payload, "Retry playback cache fill", background.VisibilityAdmin, cause)
}

func (s *Service) enqueuePromotion(payload PromotionPayload, label string, visibility string, cause error) bool {
	if s.runtime == nil {
		return false
	}
	attemptKey := objectKeyHash(payload.TemporaryPath)
	if payload.TemporaryPath == "" {
		// A direct origin fill has no unique temporary path. Give each new
		// playback-triggered retry cycle its own identity so a previously failed
		// job cannot permanently suppress future attempts for the same object.
		attemptKey = uuid.NewString()
	}
	key := cacheIdentity(payload.PoolID, payload.OriginMountID, payload.ObjectKey) + ":" + attemptKey
	job, _, err := s.runtime.Enqueue(context.Background(), background.JobSpec{
		Kind: "storage.cache.fill", Visibility: visibility,
		SubjectType: "storage_pool", SubjectID: fmt.Sprintf("%d", payload.PoolID),
		IdempotencyKey: "storage-cache-fill:" + key,
		Label:          label, Tasks: []background.TaskSpec{{
			Kind: "storage.cache.fill", Queue: background.QueueStorage, Phase: "Caching playback data",
			PayloadVersion: 1, Payload: payload, DedupeKey: key, Priority: 5, Required: true, Weight: 1, MaxAttempts: 4,
		}},
	})
	if err != nil {
		s.logger.Printf("component=storage_cache event=retry_enqueue_failed error=%q original_error=%q", err, cause)
		return false
	}
	return job != nil
}

func (s *Service) InvalidateFile(ctx context.Context, fileID uint) error {
	if s == nil || s.db == nil || fileID == 0 {
		return nil
	}
	var entries []models.StorageCacheEntry
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Model(&models.File{}).Where("id = ?", fileID).
			UpdateColumn("storage_cache_version", gorm.Expr("storage_cache_version + 1")).Error; err != nil {
			return err
		}
		if err := tx.Where("file_id = ?", fileID).Find(&entries).Error; err != nil {
			return err
		}
		return tx.Where("file_id = ?", fileID).Delete(&models.StorageCacheEntry{}).Error
	}); err != nil {
		return err
	}
	for _, entry := range entries {
		s.deleteEntryObject(context.WithoutCancel(ctx), entry)
	}
	return nil
}

func (s *Service) Prune(ctx context.Context) error {
	if s == nil || s.db == nil || s.storage == nil {
		return nil
	}
	var memberships []models.StoragePoolMount
	if err := s.db.WithContext(ctx).Where("role = ?", models.StoragePoolMountCache).Find(&memberships).Error; err != nil {
		return err
	}
	var combined error
	for _, membership := range memberships {
		var mount models.StorageMount
		if err := s.db.WithContext(ctx).First(&mount, membership.StorageMountID).Error; err != nil {
			combined = errors.Join(combined, err)
			continue
		}
		store, err := s.storage.Store(mount.UUID)
		if err != nil {
			continue
		}
		unlock := s.lockMembership(membership.StoragePoolID, membership.StorageMountID)
		err = s.pruneMembership(ctx, membership, store, 0)
		unlock()
		if err != nil {
			combined = errors.Join(combined, err)
			s.recordMembershipError(membership.StoragePoolID, membership.StorageMountID, err)
		} else {
			s.clearMembershipError(membership.StoragePoolID, membership.StorageMountID)
		}
	}
	if err := s.pruneOrphans(ctx); err != nil {
		combined = errors.Join(combined, err)
	}
	if janitor, ok := s.storage.Workspace().(storage.WorkspaceJanitor); ok {
		if err := janitor.CleanupTemporaryFiles(ctx, "playback-cache", time.Now().Add(-24*time.Hour)); err != nil {
			combined = errors.Join(combined, err)
		}
	}
	return combined
}

func (s *Service) MembershipStats(ctx context.Context, poolID uint) ([]MembershipStats, error) {
	var memberships []models.StoragePoolMount
	if err := s.db.WithContext(ctx).Where("storage_pool_id = ? AND role = ?", poolID, models.StoragePoolMountCache).Find(&memberships).Error; err != nil {
		return nil, err
	}
	stats := make([]MembershipStats, 0, len(memberships))
	for _, membership := range memberships {
		row := MembershipStats{MountID: membership.StorageMountID, MaxBytes: membership.CacheMaxBytes, MinimumFreePct: MinimumFreePercent}
		if err := s.db.WithContext(ctx).Model(&models.StorageCacheEntry{}).
			Select("COALESCE(SUM(size), 0)").
			Where("storage_pool_id = ? AND cache_mount_id = ?", poolID, membership.StorageMountID).
			Scan(&row.UsedBytes).Error; err != nil {
			return nil, err
		}
		if err := s.db.WithContext(ctx).Model(&models.StorageCacheEntry{}).
			Where("storage_pool_id = ? AND cache_mount_id = ?", poolID, membership.StorageMountID).
			Count(&row.EntryCount).Error; err != nil {
			return nil, err
		}
		var mount models.StorageMount
		if err := s.db.WithContext(ctx).First(&mount, membership.StorageMountID).Error; err == nil {
			if store, err := s.storage.Store(mount.UUID); err == nil {
				if reporter, ok := store.(storage.CapacityReporter); ok {
					if capacity, err := reporter.Capacity(ctx); err == nil && capacity.Total > 0 {
						row.CapacityKnown = true
						row.CapacityTotal = capacity.Total
						row.CapacityFree = capacity.Free
						row.FreePercent = float64(capacity.Free) * 100 / float64(capacity.Total)
					}
				}
			}
		}
		stats = append(stats, row)
	}
	return stats, nil
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.workers.Wait()
}

func (s *Service) targets(ctx context.Context, poolID uint, originMountID string, incoming int64) ([]target, error) {
	var rows []struct {
		MountID   uint
		MountUUID string
		MaxBytes  int64
		UsedBytes int64
	}
	err := s.db.WithContext(ctx).Table("storage_pool_mounts AS members").
		Select("members.storage_mount_id AS mount_id, mounts.uuid AS mount_uuid, members.cache_max_bytes AS max_bytes, COALESCE(SUM(entries.size), 0) AS used_bytes").
		Joins("JOIN storage_mounts AS mounts ON mounts.id = members.storage_mount_id").
		Joins("LEFT JOIN storage_cache_entries AS entries ON entries.storage_pool_id = members.storage_pool_id AND entries.cache_mount_id = members.storage_mount_id AND entries.deleted_at IS NULL").
		Where("members.storage_pool_id = ? AND members.role = ? AND members.cache_max_bytes > 0 AND mounts.mounted = ? AND (mounts.last_error IS NULL OR mounts.last_error = '') AND mounts.uuid <> ?", poolID, models.StoragePoolMountCache, true, originMountID).
		Group("members.storage_mount_id, mounts.uuid, members.cache_max_bytes").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]target, 0, len(rows))
	for _, row := range rows {
		if row.MaxBytes < incoming {
			continue
		}
		if _, err := s.storage.Store(row.MountUUID); err == nil {
			result = append(result, target(row))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left := float64(result[i].UsedBytes) / float64(result[i].MaxBytes)
		right := float64(result[j].UsedBytes) / float64(result[j].MaxBytes)
		if left == right {
			return result[i].MountID < result[j].MountID
		}
		return left < right
	})
	return result, nil
}

func (s *Service) lockMembership(poolID, mountID uint) func() {
	key := fmt.Sprintf("%d:%d", poolID, mountID)
	s.membershipMu.Lock()
	lock := s.membershipLocks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		s.membershipLocks[key] = lock
	}
	s.membershipMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func (s *Service) reserveWorkspace(ctx context.Context, incoming int64) (func(), bool) {
	reporter, ok := s.storage.Workspace().(storage.CapacityReporter)
	if !ok {
		return func() {}, true
	}
	capacity, err := reporter.Capacity(ctx)
	if err != nil || capacity.Total == 0 {
		return func() {}, true
	}
	minimum := int64(capacity.Total * MinimumFreePercent / 100)
	s.mu.Lock()
	if s.closed || int64(capacity.Free)-s.reservedWorkspaceBytes-incoming < minimum {
		s.mu.Unlock()
		return nil, false
	}
	s.reservedWorkspaceBytes += incoming
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			s.reservedWorkspaceBytes -= incoming
			s.mu.Unlock()
		})
	}, true
}

func (s *Service) resolvePoolID(ctx context.Context, originMountID string) uint {
	var row struct{ PoolID uint }
	err := s.db.WithContext(ctx).Table("storage_pool_mounts AS primary_member").
		Select("primary_member.storage_pool_id AS pool_id").
		Joins("JOIN storage_mounts AS origin ON origin.id = primary_member.storage_mount_id").
		Where("origin.uuid = ? AND primary_member.role = ? AND EXISTS (SELECT 1 FROM storage_pool_mounts cache_member WHERE cache_member.storage_pool_id = primary_member.storage_pool_id AND cache_member.role = ?)", originMountID, models.StoragePoolMountPrimary, models.StoragePoolMountCache).
		Order("primary_member.storage_pool_id ASC").Limit(1).Scan(&row).Error
	if err != nil {
		return 0
	}
	return row.PoolID
}

func (s *Service) cacheKey(poolID uint, originMountID, objectKey string) (storage.Key, error) {
	var pool models.StoragePool
	if err := s.db.Select("uuid").First(&pool, poolID).Error; err != nil {
		return storage.Key{}, err
	}
	return storage.ParseKey(fmt.Sprintf("cache/v1/%s/%s/%s", pool.UUID, originMountID, objectKeyHash(objectKey)))
}

func (s *Service) pruneMembership(ctx context.Context, membership models.StoragePoolMount, store storage.Store, incoming int64) error {
	if membership.CacheMaxBytes <= 0 || incoming > membership.CacheMaxBytes {
		return errCacheAdmissionSkipped
	}
	var used int64
	if err := s.db.WithContext(ctx).Model(&models.StorageCacheEntry{}).
		Select("COALESCE(SUM(size), 0)").
		Where("storage_pool_id = ? AND cache_mount_id = ?", membership.StoragePoolID, membership.StorageMountID).
		Scan(&used).Error; err != nil {
		return err
	}
	bytesToFree := int64(0)
	high := int64(float64(membership.CacheMaxBytes) * cacheHighWatermark)
	if used+incoming > high {
		low := int64(float64(membership.CacheMaxBytes) * cacheLowWatermark)
		bytesToFree = used + incoming - low
	}
	if reporter, ok := store.(storage.CapacityReporter); ok {
		capacity, err := reporter.Capacity(ctx)
		if err == nil && capacity.Total > 0 {
			minimum := int64(capacity.Total * MinimumFreePercent / 100)
			recovery := int64(capacity.Total * recoveryFreePercent / 100)
			projectedFree := int64(capacity.Free) - incoming
			if projectedFree < minimum && recovery-projectedFree > bytesToFree {
				bytesToFree = recovery - projectedFree
			}
		}
	}
	if bytesToFree <= 0 {
		return nil
	}
	var entries []models.StorageCacheEntry
	if err := s.db.WithContext(ctx).
		Where("storage_pool_id = ? AND cache_mount_id = ?", membership.StoragePoolID, membership.StorageMountID).
		Order("last_accessed_at ASC, id ASC").Find(&entries).Error; err != nil {
		return err
	}
	var freed int64
	for _, entry := range entries {
		key, err := storage.ParseKey(entry.CacheObjectKey)
		if err == nil {
			if err := store.Delete(ctx, key); err != nil && !errors.Is(err, storage.ErrNotFound) {
				return err
			}
		}
		if err := s.db.WithContext(ctx).Delete(&entry).Error; err != nil {
			return err
		}
		freed += entry.Size
		if freed >= bytesToFree {
			break
		}
	}
	if freed < bytesToFree && incoming > 0 {
		return errCacheAdmissionSkipped
	}
	return nil
}

func (s *Service) pruneOrphans(ctx context.Context) error {
	var entries []models.StorageCacheEntry
	err := s.db.WithContext(ctx).
		Where("NOT EXISTS (SELECT 1 FROM storage_pool_mounts members WHERE members.storage_pool_id = storage_cache_entries.storage_pool_id AND members.storage_mount_id = storage_cache_entries.cache_mount_id AND members.role = ?) OR NOT EXISTS (SELECT 1 FROM files cache_file WHERE cache_file.id = storage_cache_entries.file_id AND cache_file.storage_cache_version = storage_cache_entries.file_cache_version AND cache_file.deleted_at IS NULL)", models.StoragePoolMountCache).
		Find(&entries).Error
	if err != nil {
		return err
	}
	for _, entry := range entries {
		s.dropEntry(ctx, entry, true)
	}
	return nil
}

func (s *Service) dropEntry(ctx context.Context, entry models.StorageCacheEntry, deleteObject bool) {
	_ = s.db.WithContext(ctx).Delete(&entry).Error
	if !deleteObject {
		return
	}
	s.deleteEntryObject(ctx, entry)
}

func (s *Service) deleteEntryObject(ctx context.Context, entry models.StorageCacheEntry) {
	var mount models.StorageMount
	if err := s.db.WithContext(ctx).First(&mount, entry.CacheMountID).Error; err != nil {
		return
	}
	store, err := s.storage.Store(mount.UUID)
	if err != nil {
		return
	}
	key, err := storage.ParseKey(entry.CacheObjectKey)
	if err == nil {
		_ = store.Delete(ctx, key)
	}
}

func (s *Service) fileCacheVersion(ctx context.Context, fileID uint) (uint64, error) {
	var file struct{ StorageCacheVersion uint64 }
	if err := s.db.WithContext(ctx).Model(&models.File{}).Select("storage_cache_version").First(&file, fileID).Error; err != nil {
		return 0, err
	}
	return file.StorageCacheVersion, nil
}

func (s *Service) commitPromotion(ctx context.Context, entry *models.StorageCacheEntry) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// This no-op write serializes the generation check with invalidation. If
		// invalidation commits first the row no longer matches; if this commits
		// first, invalidation removes the newly inserted entry immediately after.
		matched := tx.Model(&models.File{}).
			Where("id = ? AND storage_cache_version = ?", entry.FileID, entry.FileCacheVersion).
			UpdateColumn("storage_cache_version", gorm.Expr("storage_cache_version"))
		if matched.Error != nil {
			return matched.Error
		}
		if matched.RowsAffected != 1 {
			return errCacheCaptureStale
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "storage_pool_id"}, {Name: "origin_mount_id"}, {Name: "object_key_hash"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"object_key", "cache_mount_id", "cache_object_key", "file_id", "file_cache_version", "size", "source_e_tag",
				"content_type", "cache_control", "source_mod_time", "last_accessed_at", "updated_at", "deleted_at",
			}),
		}).Create(entry).Error
	})
}

func (s *Service) claim(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	if _, exists := s.active[key]; exists {
		return false
	}
	s.active[key] = struct{}{}
	return true
}

func (s *Service) release(key string) {
	s.mu.Lock()
	delete(s.active, key)
	s.mu.Unlock()
}

func (s *Service) recordMembershipError(poolID, mountID uint, err error) {
	if s == nil || s.db == nil || err == nil {
		return
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 1000 {
		message = message[:1000]
	}
	_ = s.db.Model(&models.StoragePoolMount{}).
		Where("storage_pool_id = ? AND storage_mount_id = ?", poolID, mountID).
		Updates(map[string]any{"cache_last_error": message, "cache_last_error_at": time.Now().UTC()}).Error
}

func (s *Service) clearMembershipError(poolID, mountID uint) {
	_ = s.db.Model(&models.StoragePoolMount{}).
		Where("storage_pool_id = ? AND storage_mount_id = ?", poolID, mountID).
		Updates(map[string]any{"cache_last_error": "", "cache_last_error_at": nil}).Error
}

func objectKeyHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:])
}

func cacheIdentity(poolID uint, originMountID, objectKey string) string {
	return fmt.Sprintf("%d:%s:%s", poolID, originMountID, objectKeyHash(objectKey))
}
