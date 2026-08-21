package logic

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/traffic"
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTrackTrafficUsesBufferedRollupsWhenRecorderConfigured(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"),
		&gorm.Config{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.TrafficLog{}); err != nil {
		t.Fatal(err)
	}
	recorder := traffic.NewRecorder(db, traffic.Options{FlushInterval: time.Hour})
	t.Cleanup(func() { _ = recorder.Shutdown(context.Background()) })
	service := NewService(&app.Deps{DB: db, Traffic: recorder})
	for range 10 {
		service.TrackStorageTraffic(1, 2, 3, 0, 100, 4, "origin", false)
	}
	var before int64
	if err := db.Model(&models.TrafficLog{}).Count(&before).Error; err != nil {
		t.Fatal(err)
	}
	if before != 0 {
		t.Fatalf("synchronous traffic rows = %d, want 0", before)
	}
	if err := recorder.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	var row models.TrafficLog
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Bytes != 1000 || row.RequestCount != 10 || row.StorageMountUUID != "origin" {
		t.Fatalf("buffered traffic row = %#v", row)
	}
}

func TestGetTrafficStatsReturnsTotalAndSourceBreakdown(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"),
		&gorm.Config{},
	)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.TrafficLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	service := NewService(&app.Deps{DB: db})
	service.TrackTraffic(1, 2, 3, 0, 100)
	service.TrackDownloadTraffic(1, 2, 3, 50)

	now := time.Now()
	stats, err := service.GetTrafficStats(now.Add(-time.Minute), now.Add(time.Minute), 2, 1, 0, 0)
	if err != nil {
		t.Fatalf("GetTrafficStats() error = %v", err)
	}
	if sumTrafficPoints(stats.Traffic) != 150 {
		t.Fatalf("total = %d, want 150", sumTrafficPoints(stats.Traffic))
	}
	if sumTrafficPoints(stats.PlayerTraffic) != 100 {
		t.Fatalf("player = %d, want 100", sumTrafficPoints(stats.PlayerTraffic))
	}
	if sumTrafficPoints(stats.DownloadTraffic) != 50 {
		t.Fatalf("download = %d, want 50", sumTrafficPoints(stats.DownloadTraffic))
	}
}

func sumTrafficPoints(points []TrafficStatPoint) uint64 {
	var total uint64
	for _, point := range points {
		total += point.Bytes
	}
	return total
}
