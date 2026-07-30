package logic

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/models"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

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
