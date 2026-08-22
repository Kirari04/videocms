package logic

import (
	"testing"
	"time"

	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/models"
)

func TestGetStorageDeliveryStatsReturnsTimeSeriesAndRankedBreakdowns(t *testing.T) {
	db := newStorageAdminTestDB(t)
	originMount := models.StorageMount{UUID: "origin-mount", Name: "Backblaze", Provider: models.StorageProviderS3, Mounted: true}
	cacheMount := models.StorageMount{UUID: "cache-mount", Name: "Local cache", Provider: models.StorageProviderLocal, Mounted: true}
	if err := db.Create(&originMount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&cacheMount).Error; err != nil {
		t.Fatal(err)
	}
	primaryPool := models.StoragePool{UUID: "primary-pool", Name: "Primary"}
	archivePool := models.StoragePool{UUID: "archive-pool", Name: "Archive"}
	if err := db.Create(&primaryPool).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&archivePool).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.StoragePoolMount{
		StoragePoolID: primaryPool.ID, StorageMountID: cacheMount.ID, Role: models.StoragePoolMountCache,
	}).Error; err != nil {
		t.Fatal(err)
	}

	start := time.Date(2026, time.August, 22, 10, 0, 0, 0, time.UTC)
	rows := []models.TrafficLog{
		{BucketStart: start.Unix(), Bytes: 100, RequestCount: 2, StoragePoolID: primaryPool.ID, StorageMountUUID: originMount.UUID, DeliverySource: models.TrafficDeliverySourceOrigin},
		{BucketStart: start.Add(time.Minute).Unix(), Bytes: 60, RequestCount: 3, StoragePoolID: primaryPool.ID, StorageMountUUID: cacheMount.UUID, DeliverySource: models.TrafficDeliverySourceCache},
		{BucketStart: start.Add(time.Minute).Unix(), Bytes: 40, RequestCount: 1, StoragePoolID: archivePool.ID, StorageMountUUID: originMount.UUID, DeliverySource: models.TrafficDeliverySourceOrigin},
		{BucketStart: start.Add(time.Minute).Unix(), Bytes: 999, RequestCount: 9},
		{BucketStart: start.Add(-time.Minute).Unix(), Bytes: 500, RequestCount: 5, StoragePoolID: primaryPool.ID, StorageMountUUID: originMount.UUID, DeliverySource: models.TrafficDeliverySourceOrigin},
	}
	for index := range rows {
		if err := db.Create(&rows[index]).Error; err != nil {
			t.Fatal(err)
		}
	}

	stats, err := NewService(&app.Deps{DB: db}).GetStorageDeliveryStats(start, start.Add(2*time.Minute), 2)
	if err != nil {
		t.Fatal(err)
	}
	assertStorageTraffic(t, stats.Summary, 200, 6, 140, 3, 60, 3)
	if !stats.CacheConfigured {
		t.Fatal("cache configuration was not detected")
	}
	if len(stats.Traffic) != 3 {
		t.Fatalf("traffic points = %d, want 3", len(stats.Traffic))
	}
	if stats.Traffic[0].OriginBytes != 100 || stats.Traffic[0].OriginRequests != 2 {
		t.Fatalf("first traffic point = %#v", stats.Traffic[0])
	}
	if stats.Traffic[1].OriginBytes != 40 || stats.Traffic[1].CacheBytes != 60 || stats.Traffic[1].CacheRequests != 3 {
		t.Fatalf("second traffic point = %#v", stats.Traffic[1])
	}
	if stats.Traffic[2].OriginBytes != 0 || stats.Traffic[2].CacheBytes != 0 {
		t.Fatalf("empty traffic point = %#v", stats.Traffic[2])
	}

	if len(stats.Pools) != 2 || stats.Pools[0].Name != "Primary" || stats.Pools[1].Name != "Archive" {
		t.Fatalf("pool ranking = %#v", stats.Pools)
	}
	assertStorageTraffic(t, stats.Pools[0].Traffic, 160, 5, 100, 2, 60, 3)
	if len(stats.Mounts) != 2 || stats.Mounts[0].Name != "Backblaze" || stats.Mounts[1].Name != "Local cache" {
		t.Fatalf("mount ranking = %#v", stats.Mounts)
	}
	assertStorageTraffic(t, stats.Mounts[0].Traffic, 140, 3, 140, 3, 0, 0)
}

func TestGetStorageDeliveryStatsCapsChartCardinality(t *testing.T) {
	db := newStorageAdminTestDB(t)
	from := time.Date(2026, time.July, 23, 0, 0, 0, 0, time.UTC)
	stats, err := NewService(&app.Deps{DB: db}).GetStorageDeliveryStats(from, from.Add(30*24*time.Hour), 100_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Traffic) > maxStorageDeliveryPoints+1 {
		t.Fatalf("traffic points = %d, want at most %d", len(stats.Traffic), maxStorageDeliveryPoints+1)
	}
}
