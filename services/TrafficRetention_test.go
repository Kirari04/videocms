package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/background"
	"ch/kirari04/videocms/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTrafficRetentionRemovesOnlyExpiredRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.TrafficLog{}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().AddDate(0, 0, -91)
	recent := time.Now().UTC().AddDate(0, 0, -89)
	rows := []models.TrafficLog{
		{Model: models.Model{CreatedAt: &old}, Source: models.TrafficSourcePlayer, Bytes: 1},
		{Model: models.Model{CreatedAt: &recent}, Source: models.TrafficSourcePlayer, Bytes: 2},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	worker := &WorkerGroup{deps: &app.Deps{DB: db}}
	result, err := worker.trafficRetentionHandler(context.Background(), background.Task{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Phase, "1 old rows removed") {
		t.Fatalf("retention phase = %q", result.Phase)
	}
	var remaining []models.TrafficLog
	if err := db.Find(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].Bytes != 2 {
		t.Fatalf("remaining traffic = %#v", remaining)
	}
}
