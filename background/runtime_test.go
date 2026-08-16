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
	if !first.CanCancel {
		t.Fatal("newly queued job should advertise cancellation")
	}
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

func TestEnqueueAllowsEmptyAndNormalizesLongIdempotencyKeys(t *testing.T) {
	runtime, _ := testRuntime(t)
	ownerID := uint(7)
	for index := 0; index < 2; index++ {
		if _, reused, err := runtime.Enqueue(context.Background(), JobSpec{
			Kind: "test.empty", Visibility: VisibilityUser, OwnerID: &ownerID,
			Tasks: []TaskSpec{{Kind: "test.empty", Queue: QueueStorage, Required: true}},
		}); err != nil || reused {
			t.Fatalf("enqueue empty key %d: reused=%v err=%v", index, reused, err)
		}
	}
	longKey := strings.Repeat("long-key-", 80)
	first, reused, err := runtime.Enqueue(context.Background(), JobSpec{
		Kind: "test.long", Visibility: VisibilityUser, OwnerID: &ownerID, IdempotencyKey: longKey,
		Tasks: []TaskSpec{{Kind: "test.long", Queue: QueueStorage, Payload: map[string]any{"value": 1}, Required: true}},
	})
	if err != nil || reused {
		t.Fatalf("enqueue long key: reused=%v err=%v", reused, err)
	}
	second, reused, err := runtime.Enqueue(context.Background(), JobSpec{
		Kind: "test.long", Visibility: VisibilityUser, OwnerID: &ownerID, IdempotencyKey: longKey,
		Tasks: []TaskSpec{{Kind: "test.long", Queue: QueueStorage, Payload: map[string]any{"value": 1}, Required: true}},
	})
	if err != nil || !reused || second.ID != first.ID {
		t.Fatalf("replay long key: reused=%v ids=%s/%s err=%v", reused, first.ID, second.ID, err)
	}
}

func TestEnqueueRejectsChangedIdempotentPayload(t *testing.T) {
	runtime, _ := testRuntime(t)
	ownerID := uint(7)
	spec := JobSpec{Kind: "content.delete", Visibility: VisibilityUser, OwnerID: &ownerID, IdempotencyKey: "delete-request",
		SubjectType: "selection", SubjectID: "request", Tasks: []TaskSpec{{Kind: "content.delete", Queue: QueueStorage, Payload: map[string]any{"linkIds": []uint{1}}, Required: true}}}
	if _, _, err := runtime.Enqueue(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	spec.Tasks[0].Payload = map[string]any{"linkIds": []uint{2}}
	if _, _, err := runtime.Enqueue(context.Background(), spec); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}

func TestUserCannotReadOwnerAssociatedSystemJob(t *testing.T) {
	runtime, _ := testRuntime(t)
	ownerID := uint(7)
	job, _, err := runtime.Enqueue(context.Background(), JobSpec{
		Kind: "test.system", Visibility: VisibilitySystem, OwnerID: &ownerID, IdempotencyKey: "system-owner",
		Tasks: []TaskSpec{{Kind: "test.system", Queue: QueueAudit, Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Job(context.Background(), job.ID, &ownerID, false); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("owner accessed system job: %v", err)
	}
}

func TestReportProgressUpdatesParentBeforeCompletion(t *testing.T) {
	runtime, _ := testRuntime(t)
	started := make(chan struct{})
	release := make(chan struct{})
	if err := runtime.Register("test.progress", func(ctx context.Context, _ Task) (Result, error) {
		ReportProgress(ctx, 0.42, "Working")
		close(started)
		<-release
		return Result{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	startTestRuntime(t, runtime)
	job := enqueueTestJob(t, runtime, "live-progress", "test.progress", QueueStorage, 1)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not report progress")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		detail, err := runtime.Job(context.Background(), job.ID, nil, true)
		if err == nil && detail.Progress == 4200 {
			close(release)
			waitForJob(t, runtime, job.ID, JobSucceeded)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(release)
	t.Fatal("parent job progress was not updated while the task was running")
}

func TestOptionalChildFailureProducesUsableResultWithWarnings(t *testing.T) {
	runtime, _ := testRuntime(t)
	if err := runtime.Register("test.import", func(context.Context, Task) (Result, error) {
		return Result{ResultType: "link", ResultID: "video-1", Children: []TaskSpec{{Kind: "test.optional", Queue: QueueStorage, Required: false}}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Register("test.optional", func(context.Context, Task) (Result, error) {
		return Result{}, Permanent("derived_failed", "Derived media failed", errors.New("boom"))
	}); err != nil {
		t.Fatal(err)
	}
	startTestRuntime(t, runtime)
	job := enqueueTestJob(t, runtime, "optional-warning", "test.import", QueueStorage, 1)
	detail := waitForJob(t, runtime, job.ID, JobSucceededWithWarnings)
	if detail.ResultID != "video-1" || detail.Progress != 10000 {
		t.Fatalf("warning job lost usable result: result=%q progress=%d", detail.ResultID, detail.Progress)
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
	ownerID := uint(7)
	replayed, reused, err := runtime.Enqueue(context.Background(), JobSpec{
		Kind: "test.parent", Visibility: VisibilityUser, OwnerID: &ownerID, IdempotencyKey: "parent-child", Label: "parent-child",
		Tasks: []TaskSpec{{Kind: "test.parent", Queue: QueueStorage, Phase: "Working", DedupeKey: "test.parent", Required: true, Weight: 1, MaxAttempts: 1}},
	})
	if err != nil || !reused || replayed == nil || replayed.ID != job.ID {
		gotID := ""
		if replayed != nil {
			gotID = replayed.ID
		}
		t.Fatalf("completed dynamic job did not replay: reused=%v id=%s err=%v", reused, gotID, err)
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

func TestPauseRunningJobCheckpointsAndResumes(t *testing.T) {
	runtime, _ := testRuntime(t)
	started := make(chan struct{})
	var calls atomic.Int32
	if err := runtime.Register("test.pause", func(ctx context.Context, _ Task) (Result, error) {
		if calls.Add(1) == 1 {
			ReportProgress(ctx, 0.42, "Checkpointed")
			close(started)
			<-ctx.Done()
			return Result{}, ctx.Err()
		}
		return Result{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	startTestRuntime(t, runtime)
	job := enqueueTestJob(t, runtime, "pause-running", "test.pause", QueueStorage, 2)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}
	if err := runtime.PauseJob(context.Background(), job.ID, 1, "admin"); err != nil {
		t.Fatalf("pause job: %v", err)
	}
	detail := waitForJob(t, runtime, job.ID, JobPaused)
	if detail.Tasks[0].Status != TaskQueued || detail.Tasks[0].Progress < 4000 || detail.Tasks[0].MaxAttempts != 3 || len(detail.Tasks[0].Attempts) != 1 || detail.Tasks[0].Attempts[0].Status != AttemptInterrupted {
		t.Fatalf("unexpected paused state: %#v", detail.Tasks[0])
	}
	if detail.CanPause || !detail.CanResume || !detail.CanCancel || detail.PausedAt == nil {
		t.Fatalf("unexpected paused capabilities: %#v", detail.Job)
	}
	time.Sleep(50 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("paused task was reclaimed; calls=%d", calls.Load())
	}
	if err := runtime.ResumeJob(context.Background(), job.ID, 1, "admin"); err != nil {
		t.Fatalf("resume job: %v", err)
	}
	detail = waitForJob(t, runtime, job.ID, JobSucceeded)
	if calls.Load() != 2 || len(detail.Tasks[0].Attempts) != 2 || detail.Tasks[0].Attempts[1].Status != AttemptSucceeded {
		t.Fatalf("unexpected resumed state: calls=%d task=%#v", calls.Load(), detail.Tasks[0])
	}
}

func TestQueuedJobCanBePausedAndCanceledWithoutExecution(t *testing.T) {
	runtime, _ := testRuntime(t)
	job := enqueueTestJob(t, runtime, "pause-queued", "test.pause.queued", QueueStorage, 1)
	if err := runtime.PauseJob(context.Background(), job.ID, 1, "admin"); err != nil {
		t.Fatalf("pause queued job: %v", err)
	}
	detail := waitForJob(t, runtime, job.ID, JobPaused)
	if detail.Tasks[0].Status != TaskQueued || detail.PausedAt == nil {
		t.Fatalf("unexpected queued pause state: %#v", detail)
	}
	if err := runtime.CancelJob(context.Background(), job.ID, 1, "admin"); err != nil {
		t.Fatalf("cancel paused job: %v", err)
	}
	detail = waitForJob(t, runtime, job.ID, JobCanceled)
	if detail.Tasks[0].Status != TaskCanceled || detail.PauseRequestedAt != nil || detail.PausedAt != nil {
		t.Fatalf("unexpected canceled pause state: %#v", detail)
	}
}

func TestPausedJobIsNotCountedAsDispatchableQueueWork(t *testing.T) {
	runtime, _ := testRuntime(t)
	job := enqueueTestJob(t, runtime, "pause-queue-summary", "test.pause.summary", QueueStorage, 1)
	if err := runtime.PauseJob(context.Background(), job.ID, 1, "admin"); err != nil {
		t.Fatalf("pause queued job: %v", err)
	}
	queues, err := runtime.QueueSummaries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, queue := range queues {
		if queue.Name == QueueStorage && queue.Waiting != 0 {
			t.Fatalf("paused task counted as waiting work: %#v", queue)
		}
	}
}

func TestPauseIsRejectedAfterIrreversibleCommit(t *testing.T) {
	runtime, _ := testRuntime(t)
	committed := make(chan struct{})
	release := make(chan struct{})
	if err := runtime.Register("test.pause.commit", func(ctx context.Context, _ Task) (Result, error) {
		if !BeginCommit(ctx, "Committing") {
			return Result{}, ctx.Err()
		}
		close(committed)
		<-release
		return Result{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	startTestRuntime(t, runtime)
	job := enqueueTestJob(t, runtime, "pause-commit", "test.pause.commit", QueueStorage, 1)
	select {
	case <-committed:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not enter commit")
	}
	if err := runtime.PauseJob(context.Background(), job.ID, 1, "admin"); !errors.Is(err, ErrCommitStarted) {
		t.Fatalf("expected commit conflict, got %v", err)
	}
	close(release)
	waitForJob(t, runtime, job.ID, JobSucceeded)
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
	detail, err := runtime.Job(context.Background(), job.ID, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if detail.CanCancel {
		t.Fatal("job in an irreversible phase advertised cancellation")
	}
	listed, err := runtime.ListJobs(context.Background(), ListFilter{IncludeSystem: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].CanCancel {
		t.Fatalf("job list cancellation capability = %#v", listed)
	}
	if err := runtime.CancelJob(context.Background(), job.ID, 1, "admin"); !errors.Is(err, ErrCommitStarted) || !errors.Is(err, ErrConflict) {
		t.Fatalf("expected commit-started cancellation conflict after deletion began, got %v", err)
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
	if detail.CanCancel {
		t.Fatal("canceled job advertised cancellation")
	}
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

func TestRecoverDoesNotAutomaticallyRepeatCommittedTask(t *testing.T) {
	runtime, db := testRuntime(t)
	job := enqueueTestJob(t, runtime, "recover-committed", "test.recover", QueueStorage, 2)
	commitStartedAt := time.Now().Add(-time.Minute)
	if err := db.Model(&Job{}).Where("id = ?", job.ID).Update("status", JobRunning).Error; err != nil {
		t.Fatal(err)
	}
	var task Task
	if err := db.First(&task, "job_id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&task).Updates(map[string]any{
		"status": TaskRunning, "attempt_count": 1, "heartbeat_at": &commitStartedAt, "commit_started_at": &commitStartedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	attempt := Attempt{ID: "committed-attempt", TaskID: task.ID, Number: 1, Status: AttemptRunning, Worker: "dead-worker", StartedAt: commitStartedAt}
	if err := db.Create(&attempt).Error; err != nil {
		t.Fatal(err)
	}
	if err := runtime.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	detail, err := runtime.Job(context.Background(), job.ID, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != JobFailed || detail.Tasks[0].Status != TaskFailed || detail.Tasks[0].ErrorCode != "commit_interrupted" || detail.Tasks[0].CommitStartedAt == nil {
		t.Fatalf("committed task was not held for review: %#v", detail)
	}
}

func TestStaleRunningTaskIsRecoveredWithoutRestart(t *testing.T) {
	runtime, db := testRuntime(t)
	job := enqueueTestJob(t, runtime, "stale", "test.stale", QueueStorage, 2)
	var task Task
	if err := db.First(&task, "job_id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	staleAt := time.Now().Add(-time.Minute)
	if err := db.Model(&task).Updates(map[string]any{"status": TaskRunning, "attempt_count": 1, "heartbeat_at": &staleAt}).Error; err != nil {
		t.Fatal(err)
	}
	attempt := Attempt{ID: "stale-attempt", TaskID: task.ID, Number: 1, Status: AttemptRunning, Worker: "lost-worker", StartedAt: staleAt}
	if err := db.Create(&attempt).Error; err != nil {
		t.Fatal(err)
	}
	if err := runtime.recoverStaleTasks(context.Background(), time.Now().Add(-30*time.Second)); err != nil {
		t.Fatal(err)
	}
	detail, err := runtime.Job(context.Background(), job.ID, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != JobQueued || detail.Tasks[0].Status != TaskQueued || detail.Tasks[0].Attempts[0].Status != AttemptInterrupted {
		t.Fatalf("unexpected stale recovery state: %#v", detail)
	}
}

func TestStaleCommittedTaskRequiresExplicitRetry(t *testing.T) {
	runtime, db := testRuntime(t)
	job := enqueueTestJob(t, runtime, "stale-committed", "test.stale", QueueStorage, 2)
	var task Task
	if err := db.First(&task, "job_id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	staleAt := time.Now().Add(-time.Minute)
	if err := db.Model(&task).Updates(map[string]any{
		"status": TaskRunning, "attempt_count": 1, "heartbeat_at": &staleAt, "commit_started_at": &staleAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	attempt := Attempt{ID: "stale-committed-attempt", TaskID: task.ID, Number: 1, Status: AttemptRunning, Worker: "lost-worker", StartedAt: staleAt}
	if err := db.Create(&attempt).Error; err != nil {
		t.Fatal(err)
	}
	if err := runtime.recoverStaleTasks(context.Background(), time.Now().Add(-30*time.Second)); err != nil {
		t.Fatal(err)
	}
	detail, err := runtime.Job(context.Background(), job.ID, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != JobFailed || detail.Tasks[0].Status != TaskFailed || detail.Tasks[0].ErrorCode != "commit_interrupted" || detail.Tasks[0].CommitStartedAt == nil {
		t.Fatalf("stale committed task was not held for review: %#v", detail)
	}
}

func TestFinishRetriesTransientDatabaseFailure(t *testing.T) {
	runtime, db := testRuntime(t)
	job := enqueueTestJob(t, runtime, "finish-retry", "test.finish", QueueStorage, 1)
	var task Task
	if err := db.First(&task, "job_id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := db.Model(&task).Updates(map[string]any{"status": TaskRunning, "attempt_count": 1, "heartbeat_at": &now}).Error; err != nil {
		t.Fatal(err)
	}
	attempt := Attempt{ID: "finish-retry-attempt", TaskID: task.ID, Number: 1, Status: AttemptRunning, Worker: "test", StartedAt: now}
	if err := db.Create(&attempt).Error; err != nil {
		t.Fatal(err)
	}
	var failures atomic.Int32
	callbackName := "test:finish_retry"
	if err := db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "background_task_attempts" && failures.Add(1) <= 2 {
			tx.AddError(errors.New("forced finish failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })
	if err := runtime.finishWithRetry(task, attempt, Result{}, nil); err != nil {
		t.Fatalf("finish did not recover: %v", err)
	}
	detail, err := runtime.Job(context.Background(), job.ID, nil, true)
	if err != nil || detail.Status != JobSucceeded {
		t.Fatalf("finish result status=%v err=%v failures=%d", detail.Status, err, failures.Load())
	}
}

func TestStartCanRetryAfterInitializationFailure(t *testing.T) {
	runtime, db := testRuntime(t)
	var failed atomic.Bool
	callbackName := "test:start_failure"
	if err := db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if failed.CompareAndSwap(false, true) {
			tx.AddError(errors.New("forced startup failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(context.Background()); err == nil {
		t.Fatal("expected first start to fail")
	}
	if runtime.started || runtime.starting {
		t.Fatal("failed start left runtime marked started")
	}
	if err := db.Callback().Query().Remove(callbackName); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("second start failed: %v", err)
	}
	if !runtime.Stop(2 * time.Second) {
		t.Fatal("runtime did not stop")
	}
}

func TestCompoundCursorDoesNotSkipEqualTimestamps(t *testing.T) {
	runtime, db := testRuntime(t)
	first := enqueueTestJob(t, runtime, "cursor-a", "test.cursor", QueueStorage, 1)
	second := enqueueTestJob(t, runtime, "cursor-b", "test.cursor", QueueStorage, 1)
	createdAt := time.Now().UTC().Truncate(time.Second)
	if err := db.Model(&Job{}).Where("id IN ?", []string{first.ID, second.ID}).Update("created_at", createdAt).Error; err != nil {
		t.Fatal(err)
	}
	page, err := runtime.ListJobs(context.Background(), ListFilter{IncludeSystem: true, Limit: 1})
	if err != nil || len(page) != 1 {
		t.Fatalf("first page: jobs=%d err=%v", len(page), err)
	}
	next, err := runtime.ListJobs(context.Background(), ListFilter{IncludeSystem: true, Limit: 1, Before: &page[0].CreatedAt, BeforeID: page[0].ID})
	if err != nil || len(next) != 1 || next[0].ID == page[0].ID {
		t.Fatalf("second page: %#v err=%v", next, err)
	}
}

func TestScheduleSuccessUsesJobFinishTime(t *testing.T) {
	runtime, db := testRuntime(t)
	finishedAt := time.Now().Add(-time.Hour).UTC()
	job := Job{ID: "schedule-job", Kind: "maintenance.test", Status: JobSucceeded, Visibility: VisibilitySystem, Label: "test", FinishedAt: &finishedAt}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	state := ScheduleState{Key: "test", Kind: job.Kind, Queue: QueueMaintenance, Enabled: true, LastJobID: job.ID}
	if err := db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	runtime.refreshScheduleOutcomes()
	if err := db.First(&state, "key = ?", state.Key).Error; err != nil {
		t.Fatal(err)
	}
	if state.LastSuccessAt == nil || !state.LastSuccessAt.Equal(finishedAt) {
		t.Fatalf("last success = %v, want %v", state.LastSuccessAt, finishedAt)
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
