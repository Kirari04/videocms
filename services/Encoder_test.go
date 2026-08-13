package services

import (
	"bytes"
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/models"
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEncoderLogsConsistentTaskLifecycle(t *testing.T) {
	worker := encoderTestWorker(t, true, 1, 1)
	var logs bytes.Buffer
	worker.encoderLogger = log.New(&logs, "", 0)
	started := make(chan EncodingTask, 1)
	worker.encodingTaskRunner = func(_ context.Context, task EncodingTask) error {
		started <- task
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go worker.Encoder(ctx)

	task := waitForEncodingStart(t, started)
	waitForEncoderIdle(t, worker)
	output := logs.String()
	for _, expected := range []string{
		"component=encoder event=task_started",
		"component=encoder event=task_completed",
		"task_type=quality",
		fmt.Sprintf("file_id=%d", task.FileID),
		`file_uuid="550e8400-e29b-41d4-a716-446655440100"`,
		fmt.Sprintf("task_id=%d", task.ID),
		`task_name="test-0"`,
		"duration_ms=",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("encoder log does not contain %q:\n%s", expected, output)
		}
	}
}

func TestEncoderPersistsAndLogsTaskFailure(t *testing.T) {
	worker := encoderTestWorker(t, true, 1, 1)
	var logs bytes.Buffer
	worker.encoderLogger = log.New(&logs, "", 0)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go worker.Encoder(ctx)

	deadline := time.Now().Add(time.Second)
	var quality models.Quality
	for {
		if err := worker.deps.DB.First(&quality).Error; err != nil {
			t.Fatalf("load quality: %v", err)
		}
		if quality.Failed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for encoding failure")
		}
		time.Sleep(5 * time.Millisecond)
	}
	waitForEncoderIdle(t, worker)

	if !strings.Contains(quality.Error, "prepare source") {
		t.Fatalf("persisted encoding error = %q, want source preparation error", quality.Error)
	}
	output := logs.String()
	if !strings.Contains(output, "component=encoder event=task_failed") ||
		!strings.Contains(output, `error="prepare source:`) {
		t.Fatalf("missing consistent failure log:\n%s", output)
	}
}

func TestEncodingDiagnosticTailIsBounded(t *testing.T) {
	tail := newEncodingDiagnosticTail(5)
	if _, err := tail.Write([]byte("1234")); err != nil {
		t.Fatalf("write diagnostic: %v", err)
	}
	if _, err := tail.Write([]byte("5678")); err != nil {
		t.Fatalf("write diagnostic: %v", err)
	}
	if got := tail.String(); got != "45678" {
		t.Fatalf("diagnostic tail = %q, want %q", got, "45678")
	}
}

func TestEncoderStartsQueuedWorkWhenEnabledAtRuntime(t *testing.T) {
	worker := encoderTestWorker(t, false, 1, 1)
	started := make(chan EncodingTask, 1)
	worker.encodingTaskRunner = func(_ context.Context, task EncodingTask) error {
		started <- task
		return nil
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
	worker.encodingTaskRunner = func(_ context.Context, task EncodingTask) error {
		started <- task
		<-release
		finished <- struct{}{}
		return nil
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
	worker.encodingTaskRunner = func(_ context.Context, task EncodingTask) error {
		started <- task
		<-release
		return nil
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
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "encoder.db")), &gorm.Config{})
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

func waitForEncoderIdle(t *testing.T, worker *WorkerGroup) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		worker.encoderMu.Lock()
		active := worker.activeEncodingJobs
		worker.encoderMu.Unlock()
		if active == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for encoder to become idle")
		}
		time.Sleep(5 * time.Millisecond)
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
