package mediacache

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ch/kirari04/videocms/background"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type cacheFixture struct {
	db          *gorm.DB
	stores      *storage.Service
	cache       *Service
	pool        models.StoragePool
	origin      models.StorageMount
	target      models.StorageMount
	file        models.File
	originStore storage.Store
}

func newCacheFixture(t *testing.T, maxBytes int64) cacheFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.StorageMount{}, &models.StoragePool{}, &models.StoragePoolMount{}, &models.StorageCacheEntry{}, &models.File{}); err != nil {
		t.Fatal(err)
	}
	origin := models.StorageMount{UUID: "origin", Name: "Origin", Provider: models.StorageProviderLocal, Mounted: true}
	target := models.StorageMount{UUID: "cache", Name: "Cache", Provider: models.StorageProviderLocal, Mounted: true, System: true}
	pool := models.StoragePool{UUID: "pool", Name: "Remote uploads"}
	if err := db.Create(&origin).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&pool).Error; err != nil {
		t.Fatal(err)
	}
	file := models.File{UUID: "cache-file", StorageID: origin.UUID, StorageState: models.FileStorageAvailable}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	for _, membership := range []models.StoragePoolMount{
		{StoragePoolID: pool.ID, StorageMountID: origin.ID, Role: models.StoragePoolMountPrimary},
		{StoragePoolID: pool.ID, StorageMountID: target.ID, Role: models.StoragePoolMountCache, CacheMaxBytes: maxBytes},
	} {
		if err := db.Create(&membership).Error; err != nil {
			t.Fatal(err)
		}
	}
	originStore, err := storage.NewLocalStore(filepath.Join(t.TempDir(), "origin"))
	if err != nil {
		t.Fatal(err)
	}
	cacheStore, err := storage.NewLocalStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := storage.NewLocalWorkspace(filepath.Join(t.TempDir(), "scratch"))
	if err != nil {
		t.Fatal(err)
	}
	stores, err := storage.NewServiceWithWorkspace("origin", storage.LegacyMediaLayout{}, workspace, map[string]storage.Store{
		"origin": originStore, "cache": cacheStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	cache := New(db, stores, nil)
	t.Cleanup(func() { cache.Close(); _ = stores.Close() })
	return cacheFixture{db: db, stores: stores, cache: cache, pool: pool, origin: origin, target: target, file: file, originStore: originStore}
}

func TestReadThroughCachesCompletedPlaybackAndFallsBackToCache(t *testing.T) {
	fixture := newCacheFixture(t, 1024*1024)
	key, _ := storage.ParseKey("video/1080p/out1.ts")
	body := []byte("playback-segment")
	expected := int64(len(body))
	if _, err := fixture.originStore.Put(context.Background(), key, bytesReader(body), storage.PutOptions{ExpectedSize: &expected}); err != nil {
		t.Fatal(err)
	}
	object, hit, err := fixture.cache.Open(context.Background(), OpenRequest{
		PoolID: fixture.pool.ID, OriginMountID: fixture.origin.UUID, FileID: fixture.file.ID, Key: key,
	})
	if err != nil || hit {
		t.Fatalf("first open hit=%v err=%v", hit, err)
	}
	read, err := io.ReadAll(object.Body)
	if err != nil || string(read) != string(body) {
		t.Fatalf("first read=%q err=%v", read, err)
	}
	if err := object.Body.Close(); err != nil {
		t.Fatal(err)
	}
	waitForCacheEntries(t, fixture.db, 1)
	if err := fixture.originStore.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}

	object, hit, err = fixture.cache.Open(context.Background(), OpenRequest{
		PoolID: fixture.pool.ID, OriginMountID: fixture.origin.UUID, FileID: fixture.file.ID, Key: key,
	})
	if err != nil || !hit {
		t.Fatalf("cached open hit=%v err=%v", hit, err)
	}
	read, err = io.ReadAll(object.Body)
	if closeErr := object.Body.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil || string(read) != string(body) {
		t.Fatalf("cached read=%q err=%v", read, err)
	}
}

func TestReadThroughCachesLegacyFileWithNullGeneration(t *testing.T) {
	fixture := newCacheFixture(t, 1024*1024)
	if err := fixture.db.Exec("UPDATE files SET storage_cache_version = NULL WHERE id = ?", fixture.file.ID).Error; err != nil {
		t.Fatal(err)
	}
	key, _ := storage.ParseKey("video/1080p/legacy.ts")
	body := []byte("legacy-playback-segment")
	expected := int64(len(body))
	if _, err := fixture.originStore.Put(context.Background(), key, bytesReader(body), storage.PutOptions{ExpectedSize: &expected}); err != nil {
		t.Fatal(err)
	}

	object, result, err := fixture.cache.OpenWithResult(context.Background(), OpenRequest{
		PoolID: fixture.pool.ID, OriginMountID: fixture.origin.UUID, FileID: fixture.file.ID, Key: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CacheStatus != CacheStatusFilling {
		t.Fatalf("legacy cache status = %q, want %q", result.CacheStatus, CacheStatusFilling)
	}
	if _, err := io.ReadAll(object.Body); err != nil {
		t.Fatal(err)
	}
	if err := object.Body.Close(); err != nil {
		t.Fatal(err)
	}
	waitForCacheEntries(t, fixture.db, 1)
}

func TestOpenWithResultReportsTheMountThatServedPlayback(t *testing.T) {
	fixture := newCacheFixture(t, 1024*1024)
	key, _ := storage.ParseKey("video/1080p/attributed.ts")
	body := []byte("attributed-playback")
	expected := int64(len(body))
	if _, err := fixture.originStore.Put(context.Background(), key, bytesReader(body), storage.PutOptions{ExpectedSize: &expected}); err != nil {
		t.Fatal(err)
	}

	object, result, err := fixture.cache.OpenWithResult(context.Background(), OpenRequest{
		PoolID: fixture.pool.ID, OriginMountID: fixture.origin.UUID, FileID: fixture.file.ID, Key: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CacheHit || result.CacheStatus != CacheStatusFilling || result.PoolID != fixture.pool.ID || result.MountUUID != fixture.origin.UUID {
		t.Fatalf("origin result = %#v", result)
	}
	if _, err := io.ReadAll(object.Body); err != nil {
		t.Fatal(err)
	}
	if err := object.Body.Close(); err != nil {
		t.Fatal(err)
	}
	waitForCacheEntries(t, fixture.db, 1)

	object, result, err = fixture.cache.OpenWithResult(context.Background(), OpenRequest{
		PoolID: fixture.pool.ID, OriginMountID: fixture.origin.UUID, FileID: fixture.file.ID, Key: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer object.Body.Close()
	if !result.CacheHit || result.CacheStatus != CacheStatusHit || result.PoolID != fixture.pool.ID || result.MountUUID != fixture.target.UUID {
		t.Fatalf("cache result = %#v", result)
	}
}

func TestReadThroughCompletesPartialRangeFromOrigin(t *testing.T) {
	fixture := newCacheFixture(t, 1024*1024)
	key, _ := storage.ParseKey("video/1080p/out2.ts")
	body := []byte("playback-segment")
	expected := int64(len(body))
	if _, err := fixture.originStore.Put(context.Background(), key, bytesReader(body), storage.PutOptions{ExpectedSize: &expected}); err != nil {
		t.Fatal(err)
	}
	object, _, err := fixture.cache.Open(context.Background(), OpenRequest{
		PoolID: fixture.pool.ID, OriginMountID: fixture.origin.UUID, FileID: fixture.file.ID, Key: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := object.Body.Seek(2, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 4)
	if _, err := object.Body.Read(buffer); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if err := object.Body.Close(); err != nil {
		t.Fatal(err)
	}
	waitForCacheEntries(t, fixture.db, 1)
	if err := fixture.originStore.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	object, hit, err := fixture.cache.Open(context.Background(), OpenRequest{
		PoolID: fixture.pool.ID, OriginMountID: fixture.origin.UUID, FileID: fixture.file.ID, Key: key,
	})
	if err != nil || !hit {
		t.Fatalf("cached open after partial range hit=%v err=%v", hit, err)
	}
	defer object.Body.Close()
	read, err := io.ReadAll(object.Body)
	if err != nil || string(read) != string(body) {
		t.Fatalf("cached read=%q err=%v", read, err)
	}
}

func TestReadThroughDoesNotFillForAnUnreadObject(t *testing.T) {
	fixture := newCacheFixture(t, 1024*1024)
	key, _ := storage.ParseKey("video/1080p/unread.ts")
	body := []byte("playback-segment")
	expected := int64(len(body))
	if _, err := fixture.originStore.Put(context.Background(), key, bytesReader(body), storage.PutOptions{ExpectedSize: &expected}); err != nil {
		t.Fatal(err)
	}
	object, result, err := fixture.cache.OpenWithResult(context.Background(), OpenRequest{
		PoolID: fixture.pool.ID, OriginMountID: fixture.origin.UUID, FileID: fixture.file.ID, Key: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CacheStatus != CacheStatusFilling {
		t.Fatalf("cache status = %q, want %q", result.CacheStatus, CacheStatusFilling)
	}
	if err := object.Body.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	var count int64
	if err := fixture.db.Model(&models.StorageCacheEntry{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unread request created %d cache entries", count)
	}
}

func TestDirectFillFailuresCanEnqueueANewRetryCycle(t *testing.T) {
	fixture := newCacheFixture(t, 1024*1024)
	if err := background.Migrate(fixture.db); err != nil {
		t.Fatal(err)
	}
	fixture.cache.runtime = background.New(fixture.db, background.Options{})
	payload := writePromotionFile(t, fixture, "video/retryable-range.ts", 48)
	payload.TemporaryPath = ""
	for attempt := 0; attempt < 2; attempt++ {
		if !fixture.cache.enqueuePromotionRetry(payload, errors.New("origin read failed")) {
			t.Fatalf("retry cycle %d was not enqueued", attempt+1)
		}
	}
	var jobs int64
	if err := fixture.db.Model(&background.Job{}).Where("kind = ?", "storage.cache.fill").Count(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if jobs != 2 {
		t.Fatalf("retry jobs = %d, want 2 independent cycles", jobs)
	}
}

func TestInterruptedPlaybackHandsFillToDurableRuntime(t *testing.T) {
	fixture := newCacheFixture(t, 1024*1024)
	if err := background.Migrate(fixture.db); err != nil {
		t.Fatal(err)
	}
	fixture.cache.runtime = background.New(fixture.db, background.Options{})
	key, _ := storage.ParseKey("video/durable-range.ts")
	body := []byte("playback-segment")
	expected := int64(len(body))
	if _, err := fixture.originStore.Put(context.Background(), key, bytesReader(body), storage.PutOptions{ExpectedSize: &expected}); err != nil {
		t.Fatal(err)
	}
	object, _, err := fixture.cache.Open(context.Background(), OpenRequest{
		PoolID: fixture.pool.ID, OriginMountID: fixture.origin.UUID, FileID: fixture.file.ID, Key: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := object.Body.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 4)
	if _, err := object.Body.Read(buffer); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if err := object.Body.Close(); err != nil {
		t.Fatal(err)
	}
	var job background.Job
	if err := fixture.db.Where("kind = ?", "storage.cache.fill").First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.Visibility != background.VisibilitySystem || job.Label != "Fill requested playback data" {
		t.Fatalf("durable fill job = %#v", job)
	}
	var count int64
	if err := fixture.db.Model(&models.StorageCacheEntry{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("fill ran outside durable runtime: %d entries", count)
	}
}

func TestPromotionEvictsLeastRecentlyUsedEntriesBeforeQuota(t *testing.T) {
	fixture := newCacheFixture(t, 100)
	first := writePromotionFile(t, fixture, "video/first.ts", 50)
	if err := fixture.cache.Promote(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	second := writePromotionFile(t, fixture, "video/second.ts", 50)
	if err := fixture.cache.Promote(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	var entries []models.StorageCacheEntry
	if err := fixture.db.Order("id ASC").Find(&entries).Error; err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ObjectKey != second.ObjectKey {
		t.Fatalf("entries=%#v, want only newest object", entries)
	}
}

func TestPromotionResurrectsAPreviouslyEvictedEntry(t *testing.T) {
	fixture := newCacheFixture(t, 1024*1024)
	first := writePromotionFile(t, fixture, "video/replayed.ts", 40)
	first.Info.ETag = "first"
	if err := fixture.cache.Promote(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	var entry models.StorageCacheEntry
	if err := fixture.db.First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Delete(&entry).Error; err != nil {
		t.Fatal(err)
	}

	second := writePromotionFile(t, fixture, "video/replayed.ts", 48)
	second.Info.ETag = "second"
	if err := fixture.cache.Promote(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	var entries []models.StorageCacheEntry
	if err := fixture.db.Find(&entries).Error; err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Size != 48 || entries[0].SourceETag != "second" {
		t.Fatalf("resurrected entries = %#v", entries)
	}
}

func TestPromotionCapturedBeforeInvalidationIsDiscarded(t *testing.T) {
	fixture := newCacheFixture(t, 1024*1024)
	payload := writePromotionFile(t, fixture, "video/stale.ts", 48)
	if err := fixture.cache.InvalidateFile(context.Background(), fixture.file.ID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.cache.Promote(context.Background(), payload); !AdmissionSkipped(err) {
		t.Fatalf("promotion error = %v, want admission skipped", err)
	}
	var count int64
	if err := fixture.db.Model(&models.StorageCacheEntry{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("stale promotion created %d cache entries", count)
	}
}

func TestInvalidationAdvancesNullLegacyGeneration(t *testing.T) {
	fixture := newCacheFixture(t, 1024*1024)
	if err := fixture.db.Exec("UPDATE files SET storage_cache_version = NULL WHERE id = ?", fixture.file.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.cache.InvalidateFile(context.Background(), fixture.file.ID); err != nil {
		t.Fatal(err)
	}
	version, err := fixture.cache.fileCacheVersion(context.Background(), fixture.file.ID)
	if err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("invalidated generation = %d, want 1", version)
	}
}

func TestCacheHitRequiresCurrentFileGeneration(t *testing.T) {
	fixture := newCacheFixture(t, 1024*1024)
	key, _ := storage.ParseKey("video/generation.ts")
	body := []byte("current-origin")
	expected := int64(len(body))
	if _, err := fixture.originStore.Put(context.Background(), key, bytesReader(body), storage.PutOptions{ExpectedSize: &expected}); err != nil {
		t.Fatal(err)
	}
	payload := writePromotionFile(t, fixture, key.String(), len(body))
	if err := os.WriteFile(payload.TemporaryPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	payload.Info.Size = expected
	if err := fixture.cache.Promote(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&models.File{}).Where("id = ?", fixture.file.ID).
		UpdateColumn("storage_cache_version", gorm.Expr("storage_cache_version + 1")).Error; err != nil {
		t.Fatal(err)
	}

	object, hit, err := fixture.cache.Open(context.Background(), OpenRequest{
		PoolID: fixture.pool.ID, OriginMountID: fixture.origin.UUID, FileID: fixture.file.ID, Key: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if hit {
		t.Fatal("stale cache generation was served")
	}
	read, err := io.ReadAll(object.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := object.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if string(read) != string(body) {
		t.Fatalf("origin read = %q", read)
	}
}

func TestPromotionFallsBackToTheNextCacheMount(t *testing.T) {
	fixture := newCacheFixture(t, 1024*1024)
	unavailable := models.StorageMount{UUID: "cache-gone", Name: "Unavailable cache", Provider: models.StorageProviderLocal, Mounted: true}
	if err := fixture.db.Create(&unavailable).Error; err != nil {
		t.Fatal(err)
	}
	membership := models.StoragePoolMount{
		StoragePoolID: fixture.pool.ID, StorageMountID: unavailable.ID,
		Role: models.StoragePoolMountCache, CacheMaxBytes: 1024 * 1024,
	}
	if err := fixture.db.Create(&membership).Error; err != nil {
		t.Fatal(err)
	}
	payload := writePromotionFile(t, fixture, "video/fallback.ts", 48)
	payload.TargetMountID = unavailable.ID
	payload.TargetMountIDs = []uint{unavailable.ID, fixture.target.ID}
	if err := fixture.cache.Promote(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	var entry models.StorageCacheEntry
	if err := fixture.db.First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if entry.CacheMountID != fixture.target.ID {
		t.Fatalf("cache mount = %d, want fallback %d", entry.CacheMountID, fixture.target.ID)
	}
	if err := fixture.db.First(&membership, "storage_pool_id = ? AND storage_mount_id = ?", fixture.pool.ID, unavailable.ID).Error; err != nil {
		t.Fatal(err)
	}
	if membership.CacheLastError == "" {
		t.Fatal("failed preferred cache did not retain a health error")
	}
}

func writePromotionFile(t *testing.T, fixture cacheFixture, objectKey string, size int) PromotionPayload {
	t.Helper()
	path := filepath.Join(t.TempDir(), "capture")
	data := make([]byte, size)
	for index := range data {
		data[index] = byte(index)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	key, _ := storage.ParseKey(objectKey)
	return PromotionPayload{
		PoolID: fixture.pool.ID, OriginMountID: fixture.origin.UUID, FileID: fixture.file.ID,
		FileCacheVersion: fixture.file.StorageCacheVersion,
		ObjectKey:        objectKey, TargetMountID: fixture.target.ID, TemporaryPath: path,
		Info: storage.ObjectInfo{Key: key, Size: int64(size), ModTime: time.Now().UTC(), ContentType: "video/mp2t"},
	}
}

func waitForCacheEntries(t *testing.T, db *gorm.DB, expected int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		if err := db.Model(&models.StorageCacheEntry{}).Count(&count).Error; err == nil && count == expected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cache entry count did not reach %d", expected)
}

func bytesReader(value []byte) io.Reader {
	return &sliceReader{value: value}
}

type sliceReader struct{ value []byte }

func (r *sliceReader) Read(buffer []byte) (int, error) {
	if len(r.value) == 0 {
		return 0, io.EOF
	}
	count := copy(buffer, r.value)
	r.value = r.value[count:]
	return count, nil
}
