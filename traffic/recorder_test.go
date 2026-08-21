package traffic

import (
	"context"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"ch/kirari04/videocms/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRecorderAggregatesConcurrentEventsAndUpserts(t *testing.T) {
	db := recorderTestDB(t)
	now := time.Date(2026, time.August, 21, 12, 34, 56, 0, time.UTC)
	recorder := NewRecorder(db, Options{
		FlushInterval: time.Hour, Now: func() time.Time { return now }, Logger: log.New(io.Discard, "", 0),
	})
	t.Cleanup(func() { _ = recorder.Shutdown(context.Background()) })

	const goroutines = 32
	const eventsPerGoroutine = 50
	var workers sync.WaitGroup
	for range goroutines {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range eventsPerGoroutine {
				recorder.Record(Event{
					UserID: 1, FileID: 2, QualityID: 3, Source: models.TrafficSourcePlayer,
					Bytes: 128, StoragePoolID: 4, StorageMountUUID: "origin",
					DeliverySource: models.TrafficDeliverySourceOrigin,
				})
			}
		}()
	}
	workers.Wait()
	if err := recorder.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	recorder.Record(Event{
		UserID: 1, FileID: 2, QualityID: 3, Source: models.TrafficSourcePlayer,
		Bytes: 64, StoragePoolID: 4, StorageMountUUID: "origin",
		DeliverySource: models.TrafficDeliverySourceOrigin,
	})
	if err := recorder.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}

	var rows []models.TrafficLog
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("traffic rows = %d, want 1", len(rows))
	}
	wantRequests := uint64(goroutines*eventsPerGoroutine + 1)
	wantBytes := uint64(goroutines*eventsPerGoroutine*128 + 64)
	if rows[0].RequestCount != wantRequests || rows[0].Bytes != wantBytes {
		t.Fatalf("rollup = %#v, want requests=%d bytes=%d", rows[0], wantRequests, wantBytes)
	}
	if rows[0].BucketStart != now.Truncate(time.Minute).Unix() || rows[0].RollupKey == nil {
		t.Fatalf("rollup identity = bucket %d key %v", rows[0].BucketStart, rows[0].RollupKey)
	}
	status := recorder.Status()
	if status.PendingBuckets != 0 || status.FlushedRequests != wantRequests || status.LastFlushAt == nil || status.LastError != "" {
		t.Fatalf("recorder status = %#v", status)
	}
}

func TestRecorderBoundsEachSQLiteWriteTransaction(t *testing.T) {
	db := recorderTestDB(t)
	recorder := NewRecorder(db, Options{
		FlushInterval: time.Hour, FlushThreshold: DefaultFlushBatchSize + 2,
		MaxPending: DefaultFlushBatchSize + 1, Logger: log.New(io.Discard, "", 0),
	})
	t.Cleanup(func() { _ = recorder.Shutdown(context.Background()) })
	for fileID := uint(1); fileID <= DefaultFlushBatchSize+1; fileID++ {
		recorder.Record(Event{FileID: fileID, Source: models.TrafficSourcePlayer, Bytes: 1})
	}
	remaining, err := recorder.flushBatch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !remaining || recorder.Status().PendingBuckets != 1 {
		t.Fatalf("remaining=%v status=%#v, want one queued bucket", remaining, recorder.Status())
	}
	var rows int64
	if err := db.Model(&models.TrafficLog{}).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != DefaultFlushBatchSize {
		t.Fatalf("rows in first transaction = %d, want %d", rows, DefaultFlushBatchSize)
	}
}

func TestRecorderRetainsFailedBatchAndRetries(t *testing.T) {
	db := recorderTestDB(t)
	recorder := NewRecorder(db, Options{FlushInterval: time.Hour, Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { _ = recorder.Shutdown(context.Background()) })
	recorder.Record(Event{Source: models.TrafficSourcePlayer, Bytes: 100})
	if err := db.Migrator().DropTable(&models.TrafficLog{}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Flush(context.Background()); err == nil {
		t.Fatal("expected flush failure after dropping traffic table")
	}
	status := recorder.Status()
	if status.PendingBuckets != 1 || status.FlushFailures != 1 || status.LastError == "" {
		t.Fatalf("failed flush status = %#v", status)
	}
	if err := db.AutoMigrate(&models.TrafficLog{}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	var row models.TrafficLog
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Bytes != 100 || row.RequestCount != 1 || recorder.Status().LastError != "" {
		t.Fatalf("retried row=%#v status=%#v", row, recorder.Status())
	}
}

func TestRecorderBoundsDistinctPendingBuckets(t *testing.T) {
	db := recorderTestDB(t)
	recorder := NewRecorder(db, Options{FlushInterval: time.Hour, MaxPending: 1, Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { _ = recorder.Shutdown(context.Background()) })
	recorder.Record(Event{FileID: 1, Source: models.TrafficSourcePlayer, Bytes: 1})
	recorder.Record(Event{FileID: 2, Source: models.TrafficSourcePlayer, Bytes: 1})
	status := recorder.Status()
	if status.PendingBuckets != 1 || status.DroppedEvents != 1 {
		t.Fatalf("bounded status = %#v", status)
	}
}

func TestRecordDoesNotWaitForBusyDatabaseConnection(t *testing.T) {
	db := recorderTestDB(t)
	recorder := NewRecorder(db, Options{FlushInterval: time.Hour, Logger: log.New(io.Discard, "", 0)})
	t.Cleanup(func() { _ = recorder.Shutdown(context.Background()) })
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	done := make(chan struct{})
	go func() {
		recorder.Record(Event{Source: models.TrafficSourcePlayer, Bytes: 10})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		_ = tx.Rollback()
		t.Fatal("traffic recording waited for the occupied SQLite connection")
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
}

func TestRecorderShutdownFlushesPendingTraffic(t *testing.T) {
	db := recorderTestDB(t)
	recorder := NewRecorder(db, Options{FlushInterval: time.Hour, Logger: log.New(io.Discard, "", 0)})
	recorder.Record(Event{Source: models.TrafficSourcePlayer, Bytes: 42})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := recorder.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	var row models.TrafficLog
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Bytes != 42 || row.RequestCount != 1 {
		t.Fatalf("shutdown row = %#v", row)
	}
}

func recorderTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&models.TrafficLog{}); err != nil {
		t.Fatal(err)
	}
	return db
}
