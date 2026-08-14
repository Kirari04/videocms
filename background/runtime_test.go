package background

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testRuntime(t *testing.T) (*Runtime, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "background.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate background schema: %v", err)
	}
	runtime := New(db, Options{
		WorkerName: "test-worker", PollInterval: 5 * time.Millisecond,
		Capacity: func(string) int { return 2 },
	})
	return runtime, db
}

func startTestRuntime(t *testing.T, runtime *Runtime) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		if !runtime.Stop(2 * time.Second) {
			t.Errorf("runtime did not stop")
		}
	})
	if err := runtime.Start(ctx); err != nil {
		t.Fatalf("start runtime: %v", err)
	}
}

func enqueueTestJob(t *testing.T, runtime *Runtime, key, kind, queue string, maxAttempts int) *Job {
	t.Helper()
	ownerID := uint(7)
	job, _, err := runtime.Enqueue(context.Background(), JobSpec{
		Kind: kind, Visibility: VisibilityUser, OwnerID: &ownerID, IdempotencyKey: key, Label: key,
		Tasks: []TaskSpec{{Kind: kind, Queue: queue, Phase: "Working", DedupeKey: kind, Required: true, Weight: 1, MaxAttempts: maxAttempts}},
	})
	if err != nil {
		t.Fatalf("enqueue job: %v", err)
	}
	return job
}

func waitForJob(t *testing.T, runtime *Runtime, id string, want ...string) *JobDetail {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		detail, err := runtime.Job(context.Background(), id, nil, true)
		if err == nil {
			for _, status := range want {
				if detail.Status == status {
					return detail
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	detail, err := runtime.Job(context.Background(), id, nil, true)
	if err != nil {
		t.Fatalf("load timed-out job: %v", err)
	}
	t.Fatalf("job %s stayed in status %s, wanted %v", id, detail.Status, want)
	return nil
}

func TestEnqueueIsIdempotentAndOwnerScoped(t *testing.T) {
	runtime, _ := testRuntime(t)
	first := enqueueTestJob(t, runtime, "same-request", "test.noop", QueueStorage, 1)
	ownerID := uint(7)
	second, reused, err := runtime.Enqueue(context.Background(), JobSpec{
		Kind: "test.noop", Visibility: VisibilityUser, OwnerID: &ownerID, IdempotencyKey: "same-request", Label: "duplicate",
		Tasks: []TaskSpec{{Kind: "test.noop", Queue: QueueStorage, Required: true}},
	})
	if err != nil || !reused {
		t.Fatalf("expected idempotent reuse, reused=%v err=%v", reused, err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotency returned different jobs: %s != %s", first.ID, second.ID)
	}
	if _, err := runtime.Job(context.Background(), first.ID, &ownerID, false); err != nil {
		t.Fatalf("owner could not read job: %v", err)
	}
	otherID := uint(8)
	if _, err := runtime.Job(context.Background(), first.ID, &otherID, false); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected non-owner to see not found, got %v", err)
	}
}

func TestRuntimeAddsChildrenAndReducesJobResult(t *testing.T) {
	runtime, _ := testRuntime(t)
	if err := runtime.Register("test.parent", func(ctx context.Context, _ Task) (Result, error) {
		ReportProgress(ctx, 0.5, "Creating follow-up")
		return Result{
			ResultType: "link", ResultID: "result-123", Phase: "Parent complete",
			Children: []TaskSpec{{Kind: "test.child", Queue: QueueStorage, Phase: "Child work", DedupeKey: "child", Required: true, Weight: 3}},
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Register("test.child", func(context.Context, Task) (Result, error) {
		return Result{Phase: "Child complete"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	startTestRuntime(t, runtime)
	job := enqueueTestJob(t, runtime, "parent-child", "test.parent", QueueStorage, 1)
	detail := waitForJob(t, runtime, job.ID, JobSucceeded)
	if detail.Progress != 10000 || detail.ResultType != "link" || detail.ResultID != "result-123" {
		t.Fatalf("unexpected reduced result: progress=%d type=%q id=%q", detail.Progress, detail.ResultType, detail.ResultID)
	}
	if len(detail.Tasks) != 2 || detail.Tasks[1].ParentTaskID != detail.Tasks[0].ID {
		t.Fatalf("expected parent and linked child tasks, got %#v", detail.Tasks)
	}
}

func TestTransientFailureRetriesThenSucceeds(t *testing.T) {
	runtime, _ := testRuntime(t)
	var calls atomic.Int32
	if err := runtime.Register("test.retry", func(context.Context, Task) (Result, error) {
		if calls.Add(1) < 3 {
			return Result{}, &TaskError{Code: "temporary", Public: "Temporary failure", Diagnostic: "retry diagnostic", Class: ErrorTransient, RetryAfter: time.Millisecond}
		}
		return Result{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	startTestRuntime(t, runtime)
	job := enqueueTestJob(t, runtime, "retry", "test.retry", QueueNetwork, 4)
	detail := waitForJob(t, runtime, job.ID, JobSucceeded)
	if calls.Load() != 3 || len(detail.Tasks) != 1 || detail.Tasks[0].AttemptCount != 3 {
		t.Fatalf("expected three attempts, calls=%d task=%#v", calls.Load(), detail.Tasks)
	}
	if len(detail.Tasks[0].Attempts) != 3 || detail.Tasks[0].Attempts[0].Status != AttemptFailed || detail.Tasks[0].Attempts[2].Status != AttemptSucceeded {
		t.Fatalf("unexpected attempt history: %#v", detail.Tasks[0].Attempts)
	}
}

func TestPermanentFailureDoesNotRetryAndRedactsDiagnostics(t *testing.T) {
	runtime, _ := testRuntime(t)
	if err := runtime.Register("test.fail", func(context.Context, Task) (Result, error) {
		cause := fmt.Errorf("request https://alice:pw@example.test/file?token=super-secret s3://bob:key@bucket/path token=super-secret authorization: Bearer bearer-secret json={\"password\":\"json-secret\"} failed")
		return Result{}, Permanent("invalid", "Input is invalid", cause)
	}); err != nil {
		t.Fatal(err)
	}
	startTestRuntime(t, runtime)
	job := enqueueTestJob(t, runtime, "permanent", "test.fail", QueueNetwork, 4)
	detail := waitForJob(t, runtime, job.ID, JobFailed)
	attempts := detail.Tasks[0].Attempts
	if len(attempts) != 1 || attempts[0].Status != AttemptFailed {
		t.Fatalf("permanent failure retried unexpectedly: %#v", attempts)
	}
	diagnostic := attempts[0].Diagnostics
	if strings.Contains(diagnostic, "super-secret") || strings.Contains(diagnostic, "bearer-secret") || strings.Contains(diagnostic, "json-secret") || strings.Contains(diagnostic, "alice:pw") || strings.Contains(diagnostic, "bob:key") || strings.Contains(diagnostic, "?token=") || !strings.Contains(diagnostic, "[redacted]") {
		t.Fatalf("diagnostic was not safely redacted: %q", diagnostic)
	}
}

func TestCancelRunningJobPropagatesToHandler(t *testing.T) {
	runtime, _ := testRuntime(t)
	started := make(chan struct{})
	if err := runtime.Register("test.cancel", func(ctx context.Context, _ Task) (Result, error) {
		close(started)
		<-ctx.Done()
		return Result{}, ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	startTestRuntime(t, runtime)
	job := enqueueTestJob(t, runtime, "cancel", "test.cancel", QueueStorage, 1)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}
	if err := runtime.CancelJob(context.Background(), job.ID, 1, "admin"); err != nil {
		t.Fatalf("cancel job: %v", err)
	}
	detail := waitForJob(t, runtime, job.ID, JobCanceled)
	if detail.Tasks[0].Status != TaskCanceled || detail.Tasks[0].Attempts[0].Status != AttemptCanceled {
		t.Fatalf("unexpected canceled state: %#v", detail.Tasks[0])
	}
}

func TestDeletionCancellationStopsAtIrreversiblePhase(t *testing.T) {
	runtime, _ := testRuntime(t)
	phaseStarted := make(chan struct{})
	release := make(chan struct{})
	if err := runtime.Register("content.delete", func(ctx context.Context, _ Task) (Result, error) {
		if !BeginPhase(ctx, "Deleting content") {
			return Result{}, ctx.Err()
		}
		close(phaseStarted)
		<-release
		return Result{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	startTestRuntime(t, runtime)
	job := enqueueTestJob(t, runtime, "deletion-boundary", "content.delete", QueueStorage, 1)
	select {
	case <-phaseStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("deletion did not enter irreversible phase")
	}
	if err := runtime.CancelJob(context.Background(), job.ID, 1, "admin"); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected cancellation conflict after deletion began, got %v", err)
	}
	close(release)
	waitForJob(t, runtime, job.ID, JobSucceeded)
}

func TestQueuedDeletionCanBeCanceled(t *testing.T) {
	runtime, _ := testRuntime(t)
	job := enqueueTestJob(t, runtime, "deletion-queued", "content.delete", QueueStorage, 1)
	if err := runtime.CancelJob(context.Background(), job.ID, 1, "admin"); err != nil {
		t.Fatalf("cancel queued deletion: %v", err)
	}
	detail := waitForJob(t, runtime, job.ID, JobCanceled)
	if detail.Tasks[0].Status != TaskCanceled {
		t.Fatalf("queued deletion task status = %s", detail.Tasks[0].Status)
	}
}

func TestRecoverInterruptsAttemptsAndRequeuesTasks(t *testing.T) {
	runtime, db := testRuntime(t)
	job := enqueueTestJob(t, runtime, "recover", "test.recover", QueueStorage, 2)
	now := time.Now().Add(-time.Minute)
	if err := db.Model(&Job{}).Where("id = ?", job.ID).Updates(map[string]any{"status": JobRunning, "started_at": &now}).Error; err != nil {
		t.Fatal(err)
	}
	var task Task
	if err := db.First(&task, "job_id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&task).Updates(map[string]any{"status": TaskRunning, "attempt_count": 1, "started_at": &now, "heartbeat_at": &now}).Error; err != nil {
		t.Fatal(err)
	}
	attempt := Attempt{ID: "attempt-recover", TaskID: task.ID, Number: 1, Status: AttemptRunning, Worker: "dead-worker", StartedAt: now}
	if err := db.Create(&attempt).Error; err != nil {
		t.Fatal(err)
	}
	if err := runtime.Recover(context.Background()); err != nil {
		t.Fatalf("recover: %v", err)
	}
	detail, err := runtime.Job(context.Background(), job.ID, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != JobQueued || detail.Tasks[0].Status != TaskQueued || detail.Tasks[0].Attempts[0].Status != AttemptInterrupted {
		t.Fatalf("unexpected recovered state: %#v", detail)
	}
}

func TestPausedQueueDoesNotClaimUntilResumed(t *testing.T) {
	runtime, _ := testRuntime(t)
	var calls atomic.Int32
	if err := runtime.Register("test.paused", func(context.Context, Task) (Result, error) {
		calls.Add(1)
		return Result{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.ensureQueues(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetQueuePaused(context.Background(), QueueStorage, true, 1, "admin"); err != nil {
		t.Fatal(err)
	}
	startTestRuntime(t, runtime)
	job := enqueueTestJob(t, runtime, "paused", "test.paused", QueueStorage, 1)
	time.Sleep(75 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatal("paused queue executed work")
	}
	if err := runtime.SetQueuePaused(context.Background(), QueueStorage, false, 1, "admin"); err != nil {
		t.Fatal(err)
	}
	waitForJob(t, runtime, job.ID, JobSucceeded)
}

func TestPriorityAgingPreventsStarvation(t *testing.T) {
	runtime, db := testRuntime(t)
	old := enqueueTestJob(t, runtime, "old", "test.old", QueueFFmpeg, 1)
	newer := enqueueTestJob(t, runtime, "new", "test.new", QueueFFmpeg, 1)
	oldAt := time.Now().Add(-2 * time.Hour)
	if err := db.Model(&Task{}).Where("job_id = ?", old.ID).Updates(map[string]any{"priority": 0, "created_at": oldAt}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Task{}).Where("job_id = ?", newer.ID).Update("priority", 100).Error; err != nil {
		t.Fatal(err)
	}
	runtime.rootCtx = context.Background()
	task, _, err := runtime.claim(QueueFFmpeg)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if task == nil || task.JobID != old.ID {
		t.Fatalf("expected aged task %s, got %#v", old.ID, task)
	}
}

func TestRetentionUsesShortWindowOnlyForSuccessfulAuditJobs(t *testing.T) {
	runtime, db := testRuntime(t)
	oldAudit := enqueueTestJob(t, runtime, "audit-old", "audit.record", QueueAudit, 1)
	failedAudit := enqueueTestJob(t, runtime, "audit-failed", "audit.record", QueueAudit, 1)
	regular := enqueueTestJob(t, runtime, "regular", "media.process", QueueStorage, 1)
	finished := time.Now().Add(-48 * time.Hour)
	if err := db.Model(&Job{}).Where("id = ?", oldAudit.ID).Updates(map[string]any{"status": JobSucceeded, "finished_at": &finished}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Job{}).Where("id = ?", failedAudit.ID).Updates(map[string]any{"status": JobFailed, "finished_at": &finished}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Job{}).Where("id = ?", regular.ID).Updates(map[string]any{"status": JobSucceeded, "finished_at": &finished}).Error; err != nil {
		t.Fatal(err)
	}
	deleted, err := runtime.Retain(context.Background(), time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("expected one short-lived audit job deleted, got %d", deleted)
	}
	var remaining int64
	if err := db.Model(&Job{}).Where("id IN ?", []string{failedAudit.ID, regular.ID}).Count(&remaining).Error; err != nil || remaining != 2 {
		t.Fatalf("expected non-successful/regular jobs retained, count=%d err=%v", remaining, err)
	}
}
