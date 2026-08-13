package services

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/models"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEncoderStartsQueuedWorkWhenEnabledAtRuntime(t *testing.T) {
	worker := encoderTestWorker(t, false, 1, 1)
	started := make(chan EncodingTask, 1)
	worker.encodingTaskRunner = func(_ context.Context, task EncodingTask) {
		started <- task
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go worker.Encoder(ctx)

	assertNoEncodingStarted(t, started)
	setEncoderTestConfig(worker, true, 1)
	worker.NotifyEncoderConfigChanged()

	task := waitForEncodingStart(t, started)
	if task.Type != "quality" {
		t.Fatalf("started task type = %q, want quality", task.Type)
	}
}

func TestEncoderStopsClaimingWorkWhenDisabledAtRuntime(t *testing.T) {
	worker := encoderTestWorker(t, true, 1, 2)
	started := make(chan EncodingTask, 2)
	release := make(chan struct{})
	finished := make(chan struct{}, 2)
	worker.encodingTaskRunner = func(_ context.Context, task EncodingTask) {
		started <- task
		<-release
		finished <- struct{}{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go worker.Encoder(ctx)

	waitForEncodingStart(t, started)
	setEncoderTestConfig(worker, false, 1)
	worker.NotifyEncoderConfigChanged()
	release <- struct{}{}
	waitForSignal(t, finished, "first encoding to finish")
	assertNoEncodingStarted(t, started)

	setEncoderTestConfig(worker, true, 1)
	worker.NotifyEncoderConfigChanged()
	waitForEncodingStart(t, started)
	release <- struct{}{}
}

func TestEncoderAppliesRuntimeConcurrencyChanges(t *testing.T) {
	worker := encoderTestWorker(t, true, 1, 3)
	started := make(chan EncodingTask, 3)
	release := make(chan struct{})
	worker.encodingTaskRunner = func(_ context.Context, task EncodingTask) {
		started <- task
		<-release
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go worker.Encoder(ctx)

	waitForEncodingStart(t, started)
	assertNoEncodingStarted(t, started)

	setEncoderTestConfig(worker, true, 2)
	worker.NotifyEncoderConfigChanged()
	waitForEncodingStart(t, started)
	assertNoEncodingStarted(t, started)

	setEncoderTestConfig(worker, true, 1)
	worker.NotifyEncoderConfigChanged()
	release <- struct{}{}
	assertNoEncodingStarted(t, started)

	release <- struct{}{}
	waitForEncodingStart(t, started)
	release <- struct{}{}
}

func encoderTestWorker(t *testing.T, enabled bool, maxRunning, qualityCount int) *WorkerGroup {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&models.File{}, &models.Quality{}, &models.Audio{}, &models.Subtitle{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	file := models.File{
		UUID:         "550e8400-e29b-41d4-a716-446655440100",
		StorageState: models.FileStorageAvailable,
	}
	if err := db.Create(&file).Error; err != nil {
		t.Fatalf("create file: %v", err)
	}
	for i := 0; i < qualityCount; i++ {
		quality := models.Quality{
			FileID: file.ID,
			Name:   fmt.Sprintf("test-%d", i),
			Type:   "hls",
		}
		if err := db.Create(&quality).Error; err != nil {
			t.Fatalf("create quality: %v", err)
		}
	}

	deps := &app.Deps{
		DB: db,
		Snapshots: app.NewSnapshotStore(app.Snapshot{Config: config.Config{
			EncodingEnabled:   boolPointer(enabled),
			MaxRunningEncodes: int64(maxRunning),
		}}),
	}
	worker := NewWorkerGroup(deps, nil)
	worker.encoderPollInterval = time.Hour
	return worker
}

func setEncoderTestConfig(worker *WorkerGroup, enabled bool, maxRunning int) {
	worker.deps.Snapshots.Replace(app.Snapshot{Config: config.Config{
		EncodingEnabled:   boolPointer(enabled),
		MaxRunningEncodes: int64(maxRunning),
	}})
}

func boolPointer(value bool) *bool {
	return &value
}

func waitForEncodingStart(t *testing.T, started <-chan EncodingTask) EncodingTask {
	t.Helper()
	select {
	case task := <-started:
		return task
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for encoding to start")
		return EncodingTask{}
	}
}

func waitForSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func assertNoEncodingStarted(t *testing.T, started <-chan EncodingTask) {
	t.Helper()
	select {
	case task := <-started:
		t.Fatalf("encoding task started unexpectedly: %#v", task)
	case <-time.After(75 * time.Millisecond):
	}
}
