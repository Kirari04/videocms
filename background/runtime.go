package background

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	errRuntimeStopping = errors.New("background runtime stopping")
	errUserCanceled    = errors.New("background job canceled")
)

type Options struct {
	WorkerName   string
	Capacity     func(queue string) int
	PollInterval time.Duration
	Logger       *log.Logger
}

type Runtime struct {
	db        *gorm.DB
	options   Options
	handlers  map[string]Handler
	schedules map[string]ScheduleDefinition

	mu      sync.Mutex
	active  map[string]activeTask
	started bool
	wake    chan struct{}
	wg      sync.WaitGroup
	rootCtx context.Context
	cancel  context.CancelCauseFunc
}

type activeTask struct {
	queue  string
	cancel context.CancelCauseFunc
}

type executionReporter struct {
	runtime *Runtime
	taskID  string
	mu      sync.Mutex
	lastAt  time.Time
	last    int
}

type executionReporterKey struct{}

func New(db *gorm.DB, options Options) *Runtime {
	if options.WorkerName == "" {
		host, _ := os.Hostname()
		options.WorkerName = host
	}
	if options.WorkerName == "" {
		options.WorkerName = "embedded"
	}
	if options.PollInterval <= 0 {
		options.PollInterval = time.Second
	}
	if options.Logger == nil {
		options.Logger = log.Default()
	}
	return &Runtime{
		db:        db,
		options:   options,
		handlers:  make(map[string]Handler),
		schedules: make(map[string]ScheduleDefinition),
		active:    make(map[string]activeTask),
		wake:      make(chan struct{}, 1),
	}
}

func (r *Runtime) DB() *gorm.DB { return r.db }

func (r *Runtime) Register(kind string, handler Handler) error {
	if kind == "" || handler == nil {
		return errors.New("background handler kind and function are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[kind]; exists {
		return fmt.Errorf("background handler %q already registered", kind)
	}
	r.handlers[kind] = handler
	return nil
}

func (r *Runtime) RegisterSchedule(definition ScheduleDefinition) error {
	if definition.Key == "" || definition.Kind == "" || definition.Queue == "" || definition.Interval <= 0 || definition.Build == nil {
		return errors.New("background schedule key, kind, queue, interval, and builder are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.schedules[definition.Key]; exists {
		return fmt.Errorf("background schedule %q already registered", definition.Key)
	}
	r.schedules[definition.Key] = definition
	return nil
}

func (r *Runtime) Start(parent context.Context) error {
	if r == nil || r.db == nil {
		return errors.New("background runtime database is unavailable")
	}
	if parent == nil {
		parent = context.Background()
	}
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return nil
	}
	r.rootCtx, r.cancel = context.WithCancelCause(parent)
	r.started = true
	r.mu.Unlock()

	if err := r.ensureQueues(); err != nil {
		return err
	}
	if err := r.Recover(r.rootCtx); err != nil {
		return err
	}
	if err := r.ensureSchedules(); err != nil {
		return err
	}
	r.wg.Add(3)
	go r.dispatchLoop()
	go r.heartbeatLoop()
	go r.scheduleLoop()
	go func() {
		<-r.rootCtx.Done()
		r.cancelActive(errRuntimeStopping)
	}()
	r.Wake()
	return nil
}

func (r *Runtime) ensureSchedules() error {
	r.mu.Lock()
	definitions := make([]ScheduleDefinition, 0, len(r.schedules))
	for _, definition := range r.schedules {
		definitions = append(definitions, definition)
	}
	r.mu.Unlock()
	now := time.Now()
	for _, definition := range definitions {
		next := now.Add(definition.Interval)
		if definition.RunOnStart {
			next = now
		}
		state := ScheduleState{Key: definition.Key, Kind: definition.Kind, Queue: definition.Queue, Enabled: true, NextRunAt: &next}
		if err := r.db.Where("key = ?", definition.Key).FirstOrCreate(&state).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) scheduleLoop() {
	defer r.wg.Done()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	r.runDueSchedules()
	for {
		select {
		case <-r.rootCtx.Done():
			return
		case <-ticker.C:
			r.refreshScheduleOutcomes()
			r.runDueSchedules()
		}
	}
}

func (r *Runtime) runDueSchedules() {
	now := time.Now()
	r.mu.Lock()
	definitions := make(map[string]ScheduleDefinition, len(r.schedules))
	for key, definition := range r.schedules {
		definitions[key] = definition
	}
	r.mu.Unlock()
	for key, definition := range definitions {
		var state ScheduleState
		if err := r.db.Where("key = ? AND enabled = ? AND (next_run_at IS NULL OR next_run_at <= ?)", key, true, now).First(&state).Error; err != nil {
			continue
		}
		next := now.Add(definition.Interval)
		claimed := r.db.Model(&ScheduleState{}).Where("key = ? AND enabled = ? AND (next_run_at IS NULL OR next_run_at <= ?)", key, true, now).Updates(map[string]any{"next_run_at": &next, "last_run_at": &now})
		if claimed.Error != nil || claimed.RowsAffected != 1 {
			continue
		}
		spec := definition.Build()
		spec.Visibility = VisibilitySystem
		spec.IdempotencyKey = fmt.Sprintf("schedule:%s:%d", key, now.UnixNano()/int64(definition.Interval))
		job, _, err := r.Enqueue(r.rootCtx, spec)
		if err != nil {
			_ = r.db.Model(&ScheduleState{}).Where("key = ?", key).Updates(map[string]any{"last_status": JobFailed, "last_error": boundedMessage(err.Error(), 512)}).Error
			continue
		}
		_ = r.db.Model(&ScheduleState{}).Where("key = ?", key).Updates(map[string]any{"last_job_id": job.ID, "last_status": job.Status, "last_error": ""}).Error
	}
}

func (r *Runtime) refreshScheduleOutcomes() {
	var states []ScheduleState
	if err := r.db.Where("last_job_id <> ''").Find(&states).Error; err != nil {
		return
	}
	now := time.Now()
	for _, state := range states {
		var job Job
		if err := r.db.First(&job, "id = ?", state.LastJobID).Error; err != nil {
			continue
		}
		updates := map[string]any{"last_status": job.Status, "last_error": job.ErrorMessage}
		if job.Status == JobSucceeded || job.Status == JobSucceededWithWarnings {
			updates["last_success_at"] = &now
		}
		_ = r.db.Model(&ScheduleState{}).Where("key = ?", state.Key).Updates(updates).Error
	}
}

func (r *Runtime) Schedules(ctx context.Context) ([]ScheduleState, error) {
	var states []ScheduleState
	err := r.db.WithContext(ctx).Order("key ASC").Find(&states).Error
	return states, err
}

func (r *Runtime) RunSchedule(ctx context.Context, key string, actorID uint, actorName string) (*Job, error) {
	r.mu.Lock()
	definition, ok := r.schedules[key]
	r.mu.Unlock()
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	spec := definition.Build()
	spec.Visibility = VisibilitySystem
	spec.IdempotencyKey = "schedule-manual:" + key + ":" + uuid.NewString()
	job, _, err := r.Enqueue(ctx, spec)
	if err != nil {
		return nil, err
	}
	_ = r.db.WithContext(ctx).Model(&ScheduleState{}).Where("key = ?", key).Updates(map[string]any{"last_job_id": job.ID, "last_status": job.Status, "last_run_at": time.Now()}).Error
	_ = addEvent(r.db.WithContext(ctx), Event{JobID: job.ID, Type: "schedule_run_requested", ActorID: optionalActor(actorID), ActorName: actorName, Message: "Schedule run requested"})
	return job, nil
}

func (r *Runtime) Stop(timeout time.Duration) bool {
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel(errRuntimeStopping)
	}
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	if timeout <= 0 {
		<-done
		return true
	}
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (r *Runtime) Wake() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *Runtime) Recover(ctx context.Context) error {
	now := time.Now()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Attempt{}).
			Where("status = ?", AttemptRunning).
			Updates(map[string]any{
				"status":        AttemptInterrupted,
				"error_code":    "worker_interrupted",
				"error_message": "The worker stopped before the attempt completed",
				"finished_at":   &now,
			}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Task{}).
			Where("status = ?", TaskCancelRequested).
			Updates(map[string]any{
				"status":        TaskCanceled,
				"error_code":    "canceled",
				"error_message": "Canceled before application restart",
				"finished_at":   &now,
				"heartbeat_at":  nil,
			}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Task{}).
			Where("status = ?", TaskRunning).
			Updates(map[string]any{
				"status":              TaskQueued,
				"run_after":           nil,
				"heartbeat_at":        nil,
				"error_code":          "worker_interrupted",
				"error_message":       "Interrupted by application restart; queued again",
				"cancel_requested_at": nil,
			}).Error; err != nil {
			return err
		}
		var jobs []Job
		if err := tx.Where("status IN ?", []string{JobRunning, JobRetryWait, JobCancelRequested}).Find(&jobs).Error; err != nil {
			return err
		}
		for index := range jobs {
			if err := recomputeJob(tx, &jobs[index], now); err != nil {
				return err
			}
			if err := addEvent(tx, Event{JobID: jobs[index].ID, Type: "job_recovered", Message: "Recovered after application restart"}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Runtime) dispatchLoop() {
	defer r.wg.Done()
	ticker := time.NewTicker(r.options.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.rootCtx.Done():
			return
		case <-ticker.C:
		case <-r.wake:
		}
		r.dispatchAvailable()
	}
}

func (r *Runtime) dispatchAvailable() {
	queues := []string{QueueFFmpeg, QueueNetwork, QueueStorage, QueueMaintenance, QueueAudit}
	for _, queue := range queues {
		if r.rootCtx.Err() != nil || r.queuePaused(queue) {
			continue
		}
		available := r.capacity(queue) - r.activeCount(queue)
		for index := 0; index < available; index++ {
			task, attempt, err := r.claim(queue)
			if err != nil {
				r.options.Logger.Printf("component=background event=claim_failed queue=%s error=%q", queue, err)
				break
			}
			if task == nil {
				break
			}
			r.launch(*task, *attempt)
		}
	}
}

func (r *Runtime) claim(queue string) (*Task, *Attempt, error) {
	var task Task
	var attempt Attempt
	now := time.Now()
	err := r.db.WithContext(r.rootCtx).Transaction(func(tx *gorm.DB) error {
		if err := tx.
			Where("queue = ? AND status IN ? AND (run_after IS NULL OR run_after <= ?)", queue, []string{TaskQueued, TaskRetryWait}, now).
			Order("priority + CAST((julianday(CURRENT_TIMESTAMP) - julianday(created_at)) * 1440 AS INTEGER) DESC").
			Order("created_at ASC, id ASC").
			First(&task).Error; err != nil {
			return err
		}
		startedAt := now
		updated := tx.Model(&Task{}).
			Where("id = ? AND status IN ?", task.ID, []string{TaskQueued, TaskRetryWait}).
			Updates(map[string]any{
				"status":        TaskRunning,
				"attempt_count": gorm.Expr("attempt_count + 1"),
				"started_at":    gorm.Expr("COALESCE(started_at, ?)", startedAt),
				"heartbeat_at":  &now,
				"run_after":     nil,
				"error_code":    "",
				"error_message": "",
				"finished_at":   nil,
			})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		task.Status = TaskRunning
		task.AttemptCount++
		task.StartedAt = &startedAt
		attempt = Attempt{
			ID:        uuid.NewString(),
			TaskID:    task.ID,
			Number:    task.AttemptCount,
			Status:    AttemptRunning,
			Worker:    r.options.WorkerName,
			StartedAt: now,
		}
		if err := tx.Create(&attempt).Error; err != nil {
			return err
		}
		var job Job
		if err := tx.First(&job, "id = ?", task.JobID).Error; err != nil {
			return err
		}
		if err := recomputeJob(tx, &job, now); err != nil {
			return err
		}
		return addEvent(tx, Event{JobID: task.JobID, TaskID: task.ID, Type: "task_started", Message: fmt.Sprintf("Attempt %d started", attempt.Number)})
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &task, &attempt, nil
}

func (r *Runtime) launch(task Task, attempt Attempt) {
	ctx, cancel := context.WithCancelCause(r.rootCtx)
	reporter := &executionReporter{runtime: r, taskID: task.ID, last: task.Progress}
	ctx = context.WithValue(ctx, executionReporterKey{}, reporter)
	r.mu.Lock()
	r.active[task.ID] = activeTask{queue: task.Queue, cancel: cancel}
	handler := r.handlers[task.Kind]
	r.mu.Unlock()

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer cancel(nil)
		defer func() {
			r.mu.Lock()
			delete(r.active, task.ID)
			r.mu.Unlock()
			r.Wake()
		}()

		var result Result
		var runErr error
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					runErr = &TaskError{
						Code:       "worker_panic",
						Public:     "The worker encountered an internal error",
						Diagnostic: RedactDiagnostic(fmt.Sprintf("panic: %v\n%s", recovered, debug.Stack())),
						Class:      ErrorTransient,
					}
				}
			}()
			if handler == nil {
				runErr = Permanent("handler_missing", "No handler is registered for this task", fmt.Errorf("missing handler %s", task.Kind))
				return
			}
			result, runErr = handler(ctx, task)
		}()
		if runErr == nil && ctx.Err() != nil {
			runErr = ctx.Err()
		}
		if err := r.finish(task, attempt, result, classifyError(ctx, runErr)); err != nil {
			r.options.Logger.Printf("component=background event=finish_failed job=%s task=%s error=%q", task.JobID, task.ID, err)
		}
	}()
}

func (r *Runtime) finish(task Task, attempt Attempt, result Result, taskErr *TaskError) error {
	now := time.Now()
	return r.db.Transaction(func(tx *gorm.DB) error {
		var current Task
		if err := tx.First(&current, "id = ?", task.ID).Error; err != nil {
			return err
		}
		if current.Status != TaskRunning && current.Status != TaskCancelRequested {
			return nil
		}

		attemptStatus := AttemptSucceeded
		taskStatus := TaskSucceeded
		updates := map[string]any{
			"status":        taskStatus,
			"progress":      10000,
			"heartbeat_at":  nil,
			"finished_at":   &now,
			"error_code":    "",
			"error_message": "",
		}
		eventType := "task_succeeded"
		eventMessage := "Task completed"

		if taskErr != nil {
			code := boundedMessage(taskErr.Code, 80)
			public := boundedMessage(taskErr.Public, 512)
			if code == "" {
				code = "task_failed"
			}
			if public == "" {
				public = "The background task failed"
			}
			updates["error_code"] = code
			updates["error_message"] = public
			updates["progress"] = current.Progress
			switch taskErr.Class {
			case ErrorInterrupted:
				attemptStatus = AttemptInterrupted
				taskStatus = TaskQueued
				updates["status"] = taskStatus
				updates["run_after"] = nil
				updates["finished_at"] = nil
				updates["cancel_requested_at"] = nil
				eventType = "task_interrupted"
				eventMessage = "Task interrupted and queued again"
			case ErrorCanceled:
				attemptStatus = AttemptCanceled
				taskStatus = TaskCanceled
				updates["status"] = taskStatus
				eventType = "task_canceled"
				eventMessage = "Task canceled"
			case ErrorTransient:
				attemptStatus = AttemptFailed
				if current.AttemptCount < current.MaxAttempts {
					delay := retryDelay(current.AttemptCount, taskErr.RetryAfter)
					runAfter := now.Add(delay)
					taskStatus = TaskRetryWait
					updates["status"] = taskStatus
					updates["run_after"] = &runAfter
					updates["finished_at"] = nil
					eventType = "task_retry_scheduled"
					eventMessage = fmt.Sprintf("Retry scheduled in %s", delay.Round(time.Second))
				} else {
					taskStatus = TaskFailed
					updates["status"] = taskStatus
					eventType = "task_failed"
					eventMessage = "Task exhausted its automatic retries"
				}
			default:
				attemptStatus = AttemptFailed
				taskStatus = TaskFailed
				updates["status"] = taskStatus
				eventType = "task_failed"
				eventMessage = "Task failed"
			}
			if err := tx.Model(&Attempt{}).Where("id = ?", attempt.ID).Updates(map[string]any{
				"status":        attemptStatus,
				"error_code":    code,
				"error_message": public,
				"diagnostics":   RedactDiagnostic(taskErr.Diagnostic),
				"finished_at":   &now,
			}).Error; err != nil {
				return err
			}
		} else {
			if result.Value != nil {
				encoded, err := json.Marshal(result.Value)
				if err != nil {
					return err
				}
				updates["result"] = string(encoded)
			}
			if result.Phase != "" {
				updates["phase"] = boundedMessage(result.Phase, 120)
			}
			if err := tx.Model(&Attempt{}).Where("id = ?", attempt.ID).Updates(map[string]any{
				"status":      AttemptSucceeded,
				"finished_at": &now,
			}).Error; err != nil {
				return err
			}
		}

		if err := tx.Model(&Task{}).Where("id = ?", task.ID).Updates(updates).Error; err != nil {
			return err
		}
		if taskErr == nil {
			for index, child := range result.Children {
				child.ParentTaskID = task.ID
				if child.DedupeKey == "" {
					child.DedupeKey = fmt.Sprintf("%s:%s:%d", task.ID, child.Kind, index)
				}
				if _, err := createTask(tx, task.JobID, child); err != nil {
					if !isUniqueConstraint(err) {
						return err
					}
				}
			}
		}
		if err := addEvent(tx, Event{JobID: task.JobID, TaskID: task.ID, Type: eventType, Message: eventMessage}); err != nil {
			return err
		}
		var job Job
		if err := tx.First(&job, "id = ?", task.JobID).Error; err != nil {
			return err
		}
		if taskErr == nil {
			if result.ResultType != "" {
				job.ResultType = boundedMessage(result.ResultType, 64)
			}
			if result.ResultID != "" {
				job.ResultID = boundedMessage(result.ResultID, 128)
			}
			if job.ResultType != "" || job.ResultID != "" {
				if err := tx.Model(&job).Updates(map[string]any{"result_type": job.ResultType, "result_id": job.ResultID}).Error; err != nil {
					return err
				}
			}
		}
		return recomputeJob(tx, &job, now)
	})
}

func retryDelay(attempt int, requested time.Duration) time.Duration {
	if requested > 0 {
		if requested > 15*time.Minute {
			return 15 * time.Minute
		}
		return requested
	}
	switch attempt {
	case 1:
		return 5 * time.Second
	case 2:
		return 30 * time.Second
	default:
		return 2 * time.Minute
	}
}

func isUniqueConstraint(err error) bool {
	return err != nil && (errors.Is(err, gorm.ErrDuplicatedKey) || containsFold(err.Error(), "unique constraint"))
}

func containsFold(value, needle string) bool {
	if len(value) < len(needle) {
		return false
	}
	for index := 0; index <= len(value)-len(needle); index++ {
		if equalFoldASCII(value[index:index+len(needle)], needle) {
			return true
		}
	}
	return false
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		ac, bc := a[index], b[index]
		if ac >= 'A' && ac <= 'Z' {
			ac += 'a' - 'A'
		}
		if bc >= 'A' && bc <= 'Z' {
			bc += 'a' - 'A'
		}
		if ac != bc {
			return false
		}
	}
	return true
}

func recomputeJob(tx *gorm.DB, job *Job, now time.Time) error {
	var tasks []Task
	if err := tx.Where("job_id = ?", job.ID).Order("created_at ASC").Find(&tasks).Error; err != nil {
		return err
	}
	if len(tasks) == 0 {
		return nil
	}
	status := JobQueued
	phase := ""
	var totalWeight, completedWeight int64
	running, waiting, queued, failedRequired, failedOptional, canceled := 0, 0, 0, 0, 0, 0
	errorCode, errorMessage := "", ""
	for _, task := range tasks {
		weight := task.Weight
		if weight < 1 {
			weight = 1
		}
		totalWeight += int64(weight) * 10000
		progress := task.Progress
		if task.Status == TaskSucceeded {
			progress = 10000
		}
		if progress < 0 {
			progress = 0
		}
		if progress > 10000 {
			progress = 10000
		}
		completedWeight += int64(weight * progress)
		switch task.Status {
		case TaskRunning, TaskCancelRequested:
			running++
			if phase == "" {
				phase = task.Phase
			}
		case TaskRetryWait:
			waiting++
			if phase == "" {
				phase = task.Phase
			}
		case TaskQueued:
			queued++
			if phase == "" {
				phase = task.Phase
			}
		case TaskFailed:
			if task.Required {
				failedRequired++
			} else {
				failedOptional++
			}
			if errorCode == "" {
				errorCode, errorMessage = task.ErrorCode, task.ErrorMessage
			}
		case TaskCanceled:
			canceled++
		}
	}
	progress := job.Progress
	if totalWeight > 0 {
		calculated := int(completedWeight / (totalWeight / 10000))
		if calculated > progress {
			progress = calculated
		}
	}
	terminal := running == 0 && waiting == 0 && queued == 0
	if !terminal {
		switch {
		case job.CancelRequestedAt != nil:
			status = JobCancelRequested
		case running > 0:
			status = JobRunning
		case waiting > 0:
			status = JobRetryWait
		default:
			status = JobQueued
		}
	} else {
		switch {
		case failedRequired > 0:
			status = JobFailed
		case job.CancelRequestedAt != nil || canceled > 0:
			status = JobCanceled
		case failedOptional > 0:
			status = JobSucceededWithWarnings
		default:
			status = JobSucceeded
			progress = 10000
		}
	}
	updates := map[string]any{
		"status":        status,
		"phase":         boundedMessage(phase, 120),
		"progress":      progress,
		"error_code":    boundedMessage(errorCode, 80),
		"error_message": boundedMessage(errorMessage, 512),
	}
	if (status == JobRunning || status == JobRetryWait) && job.StartedAt == nil {
		updates["started_at"] = &now
	}
	if terminalJobStatus(status) {
		updates["finished_at"] = &now
	} else {
		updates["finished_at"] = nil
	}
	return tx.Model(&Job{}).Where("id = ?", job.ID).Updates(updates).Error
}

func ReportProgress(ctx context.Context, progress float64, message string) {
	reporter, _ := ctx.Value(executionReporterKey{}).(*executionReporter)
	if reporter == nil {
		return
	}
	value := int(progress * 10000)
	if value < 0 {
		value = 0
	}
	if value > 9999 {
		value = 9999
	}
	now := time.Now()
	reporter.mu.Lock()
	if !reporter.lastAt.IsZero() && now.Sub(reporter.lastAt) < 500*time.Millisecond && value-reporter.last < 100 {
		reporter.mu.Unlock()
		return
	}
	reporter.lastAt, reporter.last = now, value
	reporter.mu.Unlock()
	_ = reporter.runtime.db.WithContext(ctx).Model(&Task{}).Where("id = ? AND status IN ?", reporter.taskID, []string{TaskRunning, TaskCancelRequested}).Updates(map[string]any{
		"progress":         value,
		"progress_message": boundedMessage(message, 255),
		"heartbeat_at":     &now,
	}).Error
}

// BeginPhase atomically moves a running task into a new phase. Handlers use it
// as the boundary before irreversible work: cancellation either wins first
// and this returns false, or the new job phase becomes visible before an
// operator can request cancellation.
func BeginPhase(ctx context.Context, phase string) bool {
	reporter, _ := ctx.Value(executionReporterKey{}).(*executionReporter)
	if reporter == nil || strings.TrimSpace(phase) == "" {
		return false
	}
	now := time.Now()
	err := reporter.runtime.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&Task{}).Where("id = ? AND status = ?", reporter.taskID, TaskRunning).Update("phase", boundedMessage(phase, 120))
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrConflict
		}
		var task Task
		if err := tx.First(&task, "id = ?", reporter.taskID).Error; err != nil {
			return err
		}
		var job Job
		if err := tx.First(&job, "id = ?", task.JobID).Error; err != nil {
			return err
		}
		return recomputeJob(tx, &job, now)
	})
	return err == nil
}

func (r *Runtime) heartbeatLoop() {
	defer r.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.rootCtx.Done():
			return
		case now := <-ticker.C:
			r.mu.Lock()
			ids := make([]string, 0, len(r.active))
			for id := range r.active {
				ids = append(ids, id)
			}
			r.mu.Unlock()
			if len(ids) > 0 {
				_ = r.db.Model(&Task{}).Where("id IN ? AND status IN ?", ids, []string{TaskRunning, TaskCancelRequested}).Update("heartbeat_at", &now).Error
			}
		}
	}
}

func (r *Runtime) capacity(queue string) int {
	if r.options.Capacity != nil {
		if value := r.options.Capacity(queue); value > 0 {
			return value
		}
	}
	switch queue {
	case QueueStorage:
		return 2
	default:
		return 1
	}
}

func (r *Runtime) activeCount(queue string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, active := range r.active {
		if active.queue == queue {
			count++
		}
	}
	return count
}

func (r *Runtime) cancelActive(cause error) {
	r.mu.Lock()
	cancels := make([]context.CancelCauseFunc, 0, len(r.active))
	for _, active := range r.active {
		cancels = append(cancels, active.cancel)
	}
	r.mu.Unlock()
	for _, cancel := range cancels {
		cancel(cause)
	}
}

func (r *Runtime) queuePaused(queue string) bool {
	var state QueueState
	err := r.db.First(&state, "name = ?", queue).Error
	return err == nil && state.Paused
}

func (r *Runtime) ensureQueues() error {
	for _, queue := range []string{QueueFFmpeg, QueueNetwork, QueueStorage, QueueMaintenance, QueueAudit} {
		if err := r.db.FirstOrCreate(&QueueState{Name: queue}, "name = ?", queue).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) QueueSummaries(ctx context.Context) ([]QueueSummary, error) {
	var states []QueueState
	if err := r.db.WithContext(ctx).Order("name ASC").Find(&states).Error; err != nil {
		return nil, err
	}
	result := make([]QueueSummary, 0, len(states))
	for _, state := range states {
		summary := QueueSummary{QueueState: state, Capacity: r.capacity(state.Name), Active: r.activeCount(state.Name)}
		_ = r.db.WithContext(ctx).Model(&Task{}).Where("queue = ? AND status IN ?", state.Name, []string{TaskQueued, TaskRetryWait}).Count(&summary.Waiting).Error
		var oldest Task
		if err := r.db.WithContext(ctx).Where("queue = ? AND status IN ?", state.Name, []string{TaskQueued, TaskRetryWait}).Order("created_at ASC").First(&oldest).Error; err == nil {
			summary.OldestAt = &oldest.CreatedAt
		}
		result = append(result, summary)
	}
	return result, nil
}

func (r *Runtime) SetQueuePaused(ctx context.Context, queue string, paused bool, actorID uint, actorName string) error {
	now := time.Now()
	updates := map[string]any{"paused": paused, "updated_at": now}
	if paused {
		updates["paused_by"] = actorID
		updates["paused_at"] = &now
	} else {
		updates["paused_by"] = nil
		updates["paused_at"] = nil
	}
	result := r.db.WithContext(ctx).Model(&QueueState{}).Where("name = ?", queue).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	if !paused {
		r.Wake()
	}
	return nil
}

func (r *Runtime) CancelJob(ctx context.Context, jobID string, actorID uint, actorName string) error {
	now := time.Now()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job Job
		if err := tx.First(&job, "id = ?", jobID).Error; err != nil {
			return err
		}
		if terminalJobStatus(job.Status) {
			return ErrConflict
		}
		if job.Kind == "content.delete" && job.Phase != "" && job.Phase != "queued" && job.Phase != "validating" {
			return ErrConflict
		}
		if err := tx.Model(&job).Updates(map[string]any{"status": JobCancelRequested, "cancel_requested_at": &now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Task{}).Where("job_id = ? AND status IN ?", jobID, []string{TaskQueued, TaskRetryWait}).Updates(map[string]any{
			"status": TaskCanceled, "finished_at": &now, "error_code": "canceled", "error_message": "Canceled before execution",
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Task{}).Where("job_id = ? AND status = ?", jobID, TaskRunning).Updates(map[string]any{
			"status": TaskCancelRequested, "cancel_requested_at": &now,
		}).Error; err != nil {
			return err
		}
		if err := addEvent(tx, Event{JobID: jobID, Type: "job_cancel_requested", ActorID: optionalActor(actorID), ActorName: actorName, Message: "Cancellation requested"}); err != nil {
			return err
		}
		return recomputeJob(tx, &job, now)
	})
	if err != nil {
		return err
	}
	r.mu.Lock()
	var cancels []context.CancelCauseFunc
	var ids []string
	_ = r.db.WithContext(ctx).Model(&Task{}).Where("job_id = ? AND status = ?", jobID, TaskCancelRequested).Pluck("id", &ids).Error
	for _, id := range ids {
		if active, ok := r.active[id]; ok {
			cancels = append(cancels, active.cancel)
		}
	}
	r.mu.Unlock()
	for _, cancel := range cancels {
		cancel(errUserCanceled)
	}
	return nil
}

func (r *Runtime) RetryJob(ctx context.Context, jobID string, actorID uint, actorName string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job Job
		if err := tx.First(&job, "id = ?", jobID).Error; err != nil {
			return err
		}
		if job.Status != JobFailed && job.Status != JobCanceled && job.Status != JobSucceededWithWarnings {
			return ErrConflict
		}
		result := tx.Model(&Task{}).Where("job_id = ? AND status IN ?", jobID, []string{TaskFailed, TaskCanceled}).Updates(map[string]any{
			"status": TaskQueued, "run_after": nil, "finished_at": nil, "cancel_requested_at": nil, "error_code": "", "error_message": "",
			"max_attempts": gorm.Expr("attempt_count + 4"),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrConflict
		}
		if err := tx.Model(&job).Updates(map[string]any{"status": JobQueued, "finished_at": nil, "cancel_requested_at": nil, "error_code": "", "error_message": ""}).Error; err != nil {
			return err
		}
		if err := addEvent(tx, Event{JobID: jobID, Type: "job_retried", ActorID: optionalActor(actorID), ActorName: actorName, Message: "Job queued for manual retry"}); err != nil {
			return err
		}
		return recomputeJob(tx, &job, now)
	})
}

func (r *Runtime) CancelTask(ctx context.Context, taskID string, actorID uint, actorName string) error {
	now := time.Now()
	var loaded Task
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&loaded, "id = ?", taskID).Error; err != nil {
			return err
		}
		if terminalTaskStatus(loaded.Status) {
			return ErrConflict
		}
		updates := map[string]any{"status": TaskCanceled, "finished_at": &now, "error_code": "canceled", "error_message": "Canceled by administrator"}
		if loaded.Status == TaskRunning || loaded.Status == TaskCancelRequested {
			updates = map[string]any{"status": TaskCancelRequested, "cancel_requested_at": &now}
		}
		if err := tx.Model(&loaded).Updates(updates).Error; err != nil {
			return err
		}
		if err := addEvent(tx, Event{JobID: loaded.JobID, TaskID: loaded.ID, Type: "task_cancel_requested", ActorID: optionalActor(actorID), ActorName: actorName, Message: "Task cancellation requested"}); err != nil {
			return err
		}
		var job Job
		if err := tx.First(&job, "id = ?", loaded.JobID).Error; err != nil {
			return err
		}
		return recomputeJob(tx, &job, now)
	})
	if err != nil {
		return err
	}
	r.mu.Lock()
	active, ok := r.active[taskID]
	r.mu.Unlock()
	if ok {
		active.cancel(errUserCanceled)
	}
	return nil
}

func (r *Runtime) RetryTask(ctx context.Context, taskID string, actorID uint, actorName string) error {
	now := time.Now()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task Task
		if err := tx.First(&task, "id = ?", taskID).Error; err != nil {
			return err
		}
		if task.Status != TaskFailed && task.Status != TaskCanceled {
			return ErrConflict
		}
		if err := tx.Model(&task).Updates(map[string]any{
			"status": TaskQueued, "run_after": nil, "finished_at": nil, "cancel_requested_at": nil, "error_code": "", "error_message": "",
			"max_attempts": gorm.Expr("attempt_count + 4"),
		}).Error; err != nil {
			return err
		}
		if err := addEvent(tx, Event{JobID: task.JobID, TaskID: task.ID, Type: "task_retried", ActorID: optionalActor(actorID), ActorName: actorName, Message: "Task queued for manual retry"}); err != nil {
			return err
		}
		var job Job
		if err := tx.First(&job, "id = ?", task.JobID).Error; err != nil {
			return err
		}
		if err := tx.Model(&job).Updates(map[string]any{"finished_at": nil, "cancel_requested_at": nil}).Error; err != nil {
			return err
		}
		return recomputeJob(tx, &job, now)
	})
	if err == nil {
		r.Wake()
	}
	return err
}

func (r *Runtime) CancelKinds(ctx context.Context, kindPrefixes []string, actorID uint, actorName, reason string) error {
	if len(kindPrefixes) == 0 {
		return nil
	}
	query := r.db.WithContext(ctx).Model(&Task{}).Distinct("job_id").Where("status IN ?", []string{TaskQueued, TaskRunning, TaskRetryWait, TaskCancelRequested})
	kindConditions := make([]string, 0, len(kindPrefixes))
	kindArgs := make([]any, 0, len(kindPrefixes))
	for _, prefix := range kindPrefixes {
		kindConditions = append(kindConditions, "kind LIKE ?")
		kindArgs = append(kindArgs, prefix+"%")
	}
	query = query.Where("("+strings.Join(kindConditions, " OR ")+")", kindArgs...)
	var jobIDs []string
	if err := query.Pluck("job_id", &jobIDs).Error; err != nil {
		return err
	}
	for _, jobID := range jobIDs {
		if err := r.CancelJob(ctx, jobID, actorID, actorName); err != nil && !errors.Is(err, ErrConflict) {
			return err
		}
		if reason != "" {
			_ = addEvent(r.db.WithContext(ctx), Event{JobID: jobID, Type: "feature_disabled", ActorID: optionalActor(actorID), ActorName: actorName, Message: boundedMessage(reason, 512)})
		}
	}
	return nil
}

func optionalActor(id uint) *uint {
	if id == 0 {
		return nil
	}
	return &id
}

func (r *Runtime) Retain(ctx context.Context, before time.Time) (int64, error) {
	var jobs []string
	auditBefore := time.Now().Add(-24 * time.Hour)
	terminal := []string{JobSucceeded, JobSucceededWithWarnings, JobFailed, JobCanceled}
	if err := r.db.WithContext(ctx).Model(&Job{}).
		Where("status IN ?", terminal).
		Where("finished_at < ? OR (kind = ? AND status IN ? AND finished_at < ?)", before, "audit.record", []string{JobSucceeded, JobSucceededWithWarnings}, auditBefore).
		Pluck("id", &jobs).Error; err != nil {
		return 0, err
	}
	if len(jobs) == 0 {
		return 0, nil
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var taskIDs []string
		if err := tx.Model(&Task{}).Where("job_id IN ?", jobs).Pluck("id", &taskIDs).Error; err != nil {
			return err
		}
		if len(taskIDs) > 0 {
			if err := tx.Where("task_id IN ?", taskIDs).Delete(&Attempt{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("job_id IN ?", jobs).Delete(&Event{}).Error; err != nil {
			return err
		}
		if err := tx.Where("job_id IN ?", jobs).Delete(&Task{}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", jobs).Delete(&Job{}).Error
	})
	return int64(len(jobs)), err
}

var ErrConflict = errors.New("background operation conflicts with its current state")

func sortedKeys[K ~string, V any](values map[K]V) []K {
	keys := make([]K, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
