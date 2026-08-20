package mediacache

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestS3OriginPartialPlaybackFillsLocalCacheIntegration(t *testing.T) {
	endpoint := os.Getenv("VIDEOCMS_S3_INTEGRATION_ENDPOINT")
	if endpoint == "" {
		t.Skip("VIDEOCMS_S3_INTEGRATION_ENDPOINT is not configured")
	}
	bucket := os.Getenv("VIDEOCMS_S3_INTEGRATION_BUCKET")
	if bucket == "" {
		t.Fatal("VIDEOCMS_S3_INTEGRATION_BUCKET is required")
	}
	region := os.Getenv("VIDEOCMS_S3_INTEGRATION_REGION")
	if region == "" {
		region = "us-east-1"
	}
	ctx := context.Background()
	origin, err := storage.NewS3Store(ctx, storage.S3Options{
		Bucket: bucket, Region: region, Endpoint: endpoint,
		Prefix:          "videocms-cache-integration/" + uuid.NewString(),
		AccessKeyID:     os.Getenv("VIDEOCMS_S3_INTEGRATION_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("VIDEOCMS_S3_INTEGRATION_SECRET_ACCESS_KEY"),
		UsePathStyle:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cacheStore, err := storage.NewLocalStore(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := storage.NewLocalWorkspace(filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	stores, err := storage.NewServiceWithWorkspace("s3", storage.LegacyMediaLayout{}, workspace, map[string]storage.Store{
		"s3": origin, "cache": cacheStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stores.Close() })

	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.StorageMount{}, &models.StoragePool{}, &models.StoragePoolMount{},
		&models.StorageCacheEntry{}, &models.File{},
	); err != nil {
		t.Fatal(err)
	}
	originMount := models.StorageMount{UUID: "s3", Name: "S3 origin", Provider: models.StorageProviderS3, Mounted: true}
	cacheMount := models.StorageMount{UUID: "cache", Name: "Local cache", Provider: models.StorageProviderLocal, Mounted: true}
	pool := models.StoragePool{UUID: uuid.NewString(), Name: "S3 with local cache"}
	for _, value := range []any{&originMount, &cacheMount, &pool} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, membership := range []models.StoragePoolMount{
		{StoragePoolID: pool.ID, StorageMountID: originMount.ID, Role: models.StoragePoolMountPrimary},
		{StoragePoolID: pool.ID, StorageMountID: cacheMount.ID, Role: models.StoragePoolMountCache, CacheMaxBytes: 1024 * 1024},
	} {
		if err := db.Create(&membership).Error; err != nil {
			t.Fatal(err)
		}
	}
	file := models.File{UUID: uuid.NewString(), StorageID: originMount.UUID, StoragePoolID: &pool.ID, StorageState: models.FileStorageAvailable}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	service := New(db, stores, nil)
	t.Cleanup(service.Close)

	key, err := storage.ParseKey(file.UUID + "/720p/out0.ts")
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("hls-segment-"), 256)
	expected := int64(len(body))
	if _, err := origin.Put(ctx, key, bytes.NewReader(body), storage.PutOptions{ExpectedSize: &expected, ContentType: "video/mp2t"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = origin.Delete(context.Background(), key) })

	object, result, err := service.OpenWithResult(ctx, OpenRequest{
		PoolID: pool.ID, OriginMountID: originMount.UUID, FileID: file.ID, Key: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CacheStatus != CacheStatusFilling {
		t.Fatalf("first cache status = %q", result.CacheStatus)
	}
	if _, err := object.Body.Seek(64, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	partial := make([]byte, 128)
	if _, err := io.ReadFull(object.Body, partial); err != nil {
		t.Fatal(err)
	}
	if err := object.Body.Close(); err != nil {
		t.Fatal(err)
	}
	waitForCacheEntries(t, db, 1)
	if err := origin.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}

	object, result, err = service.OpenWithResult(ctx, OpenRequest{
		PoolID: pool.ID, OriginMountID: originMount.UUID, FileID: file.ID, Key: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.CacheHit || result.CacheStatus != CacheStatusHit || result.MountUUID != cacheMount.UUID {
		t.Fatalf("cache result = %#v", result)
	}
	read, err := io.ReadAll(object.Body)
	closeErr := object.Body.Close()
	if err != nil || closeErr != nil || !bytes.Equal(read, body) {
		t.Fatalf("cached read bytes=%d err=%v close=%v", len(read), err, closeErr)
	}

	stats, err := service.MembershipStats(ctx, pool.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].EntryCount != 1 || stats[0].UsedBytes != expected {
		t.Fatalf("cache stats = %#v", stats)
	}
}
