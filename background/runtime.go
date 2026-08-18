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
	errRuntimeStopping     = errors.New("background runtime stopping")
	errUserCanceled        = errors.New("background job canceled")
	errUserPaused          = errors.New("background job paused")
	ErrIdempotencyConflict = errors.New("idempotency key was already used for a different request")
	ErrConflict            = errors.New("background operation conflicts with its current state")
	ErrCommitStarted       = fmt.Errorf("%w: irreversible task commit has started", ErrConflict)
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

	mu       sync.Mutex
	active   map[string]activeTask
	loops    map[string]LoopHealth
	started  bool
	starting bool
	wake     chan struct{}
	wg       sync.WaitGroup
	rootCtx  context.Context
	cancel   context.CancelCauseFunc
}

type activeTask struct {
	queue  string
	jobID  string
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
		loops:     make(map[string]LoopHealth),
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
	if r.starting {
		r.mu.Unlock()
		return errors.New("background runtime is already starting")
	}
	r.rootCtx, r.cancel = context.WithCancelCause(parent)
	r.starting = true
	r.mu.Unlock()

	if err := r.ensureQueues(); err != nil {
		r.startFailed(err)
		return err
	}
	if err := r.Recover(r.rootCtx); err != nil {
		r.startFailed(err)
		return err
	}
	if err := r.ensureSchedules(); err != nil {
		r.startFailed(err)
		return err
	}
	r.mu.Lock()
	r.started = true
	r.starting = false
	r.mu.Unlock()
	r.wg.Add(3)
	go r.superviseLoop("dispatcher", r.dispatchLoop)
	go r.superviseLoop("heartbeat", r.heartbeatLoop)
	go r.superviseLoop("scheduler", r.scheduleLoop)
	go func() {
		<-r.rootCtx.Done()
		r.cancelActive(errRuntimeStopping)
	}()
	r.Wake()
	return nil
}

func (r *Runtime) startFailed(cause error) {
	r.mu.Lock()
	cancel := r.cancel
	r.rootCtx = nil
	r.cancel = nil
	r.starting = false
	r.started = false
	r.mu.Unlock()
	if cancel != nil {
		cancel(cause)
	}
}

func (r *Runtime) superviseLoop(name string, loop func()) {
	defer r.wg.Done()
	for r.rootCtx != nil && r.rootCtx.Err() == nil {
		now := time.Now()
		r.mu.Lock()
		health := r.loops[name]
		health.Name, health.Status, health.LastStartAt = name, "running", &now
		r.loops[name] = health
		r.mu.Unlock()

		failure := runRuntimeLoop(loop)
		if r.rootCtx == nil || r.rootCtx.Err() != nil {
			r.mu.Lock()
			health := r.loops[name]
			health.Status = "stopped"
			r.loops[name] = health
			r.mu.Unlock()
			return
		}
		if failure == "" {
			failure = "runtime loop stopped unexpectedly"
		}
		r.mu.Lock()
		health = r.loops[name]
		health.Status = "degraded"
		health.Restarts++
		health.LastError = RedactDiagnostic(failure)
		r.loops[name] = health
		r.mu.Unlock()
		r.options.Logger.Printf("component=background event=loop_restart loop=%s error=%q", name, failure)
		timer := time.NewTimer(time.Second)
		select {
		case <-r.rootCtx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func runRuntimeLoop(loop func()) (failure string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			failure = fmt.Sprintf("panic: %v\n%s", recovered, debug.Stack())
		}
	}()
	loop()
	return ""
}

func (r *Runtime) Health() RuntimeHealth {
	r.mu.Lock()
	defer r.mu.Unlock()
	status := "stopped"
	if r.starting {
		status = "starting"
	} else if r.started && r.rootCtx != nil && r.rootCtx.Err() == nil {
		status = "running"
		for _, health := range r.loops {
			if health.Status != "running" {
				status = "degraded"
				break
			}
		}
	}
	loops := make([]LoopHealth, 0, len(r.loops))
	for _, health := range r.loops {
		loops = append(loops, health)
	}
	sort.Slice(loops, func(i, j int) bool { return loops[i].Name < loops[j].Name })
	return RuntimeHealth{Status: status, Loops: loops}
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
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				r.options.Logger.Printf("component=background event=schedule_load_failed schedule=%s error=%q", key, err)
			}
			continue
		}
		next := now.Add(definition.Interval)
		claimed := r.db.Model(&ScheduleState{}).Where("key = ? AND enabled = ? AND (next_run_at IS NULL OR next_run_at <= ?)", key, true, now).Updates(map[string]any{"next_run_at": &next, "last_run_at": &now})
		if claimed.Error != nil || claimed.RowsAffected != 1 {
			if claimed.Error != nil {
				r.options.Logger.Printf("component=background event=schedule_claim_failed schedule=%s error=%q", key, claimed.Error)
			}
			continue
		}
		spec := definition.Build()
		spec.Visibility = VisibilitySystem
		spec.IdempotencyKey = fmt.Sprintf("schedule:%s:%d", key, now.UnixNano()/int64(definition.Interval))
		job, _, err := r.Enqueue(r.rootCtx, spec)
		if err != nil {
			if updateErr := r.db.Model(&ScheduleState{}).Where("key = ?", key).Updates(map[string]any{"last_status": JobFailed, "last_error": boundedMessage(err.Error(), 512)}).Error; updateErr != nil {
				r.options.Logger.Printf("component=background event=schedule_error_state_failed schedule=%s error=%q", key, updateErr)
			}
			continue
		}
		if err := r.db.Model(&ScheduleState{}).Where("key = ?", key).Updates(map[string]any{"last_job_id": job.ID, "last_status": job.Status, "last_error": ""}).Error; err != nil {
			r.options.Logger.Printf("component=background event=schedule_state_failed schedule=%s error=%q", key, err)
		}
	}
}

func (r *Runtime) refreshScheduleOutcomes() {
	var states []ScheduleState
	if err := r.db.Where("last_job_id <> ''").Find(&states).Error; err != nil {
		r.options.Logger.Printf("component=background event=schedule_refresh_failed error=%q", err)
		return
	}
	for _, state := range states {
		var job Job
		if err := r.db.First(&job, "id = ?", state.LastJobID).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				r.options.Logger.Printf("component=background event=schedule_job_load_failed schedule=%s error=%q", state.Key, err)
			}
			continue
		}
		updates := map[string]any{"last_status": job.Status, "last_error": job.ErrorMessage}
		if job.Status == JobSucceeded || job.Status == JobSucceededWithWarnings {
			updates["last_success_at"] = job.FinishedAt
		}
		if err := r.db.Model(&ScheduleState{}).Where("key = ?", state.Key).Updates(updates).Error; err != nil {
			r.options.Logger.Printf("component=background event=schedule_outcome_failed schedule=%s error=%q", state.Key, err)
		}
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
	if err := r.db.WithContext(ctx).Model(&ScheduleState{}).Where("key = ?", key).Updates(map[string]any{"last_job_id": job.ID, "last_status": job.Status, "last_run_at": time.Now()}).Error; err != nil {
		r.options.Logger.Printf("component=background event=manual_schedule_state_failed schedule=%s job=%s error=%q", key, job.ID, err)
	}
	if err := addEvent(r.db.WithContext(ctx), Event{JobID: job.ID, Type: "schedule_run_requested", ActorID: optionalActor(actorID), ActorName: actorName, Message: "Schedule run requested"}); err != nil {
		r.options.Logger.Printf("component=background event=manual_schedule_event_failed schedule=%s job=%s error=%q", key, job.ID, err)
	}
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
			Where("status IN ? AND commit_started_at IS NOT NULL", []string{TaskRunning, TaskCancelRequested}).
			Updates(map[string]any{
				"status":        TaskFailed,
				"error_code":    "commit_interrupted",
				"error_message": "The worker stopped after irreversible work began; review before retrying",
				"finished_at":   &now,
				"heartbeat_at":  nil,
			}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Task{}).
			Where("status = ? AND commit_started_at IS NULL", TaskCancelRequested).
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
			Where("status = ? AND commit_started_at IS NULL", TaskRunning).
			Updates(map[string]any{
				"status":              TaskQueued,
				"max_attempts":        gorm.Expr("max_attempts + 1"),
				"run_after":           nil,
				"heartbeat_at":        nil,
				"error_code":          "worker_interrupted",
				"error_message":       "Interrupted by application restart; queued again",
				"cancel_requested_at": nil,
			}).Error; err != nil {
			return err
		}
		var jobs []Job
		if err := tx.Where("status IN ?", []string{JobRunning, JobRetryWait, JobPauseRequested, JobPaused, JobCancelRequested}).Find(&jobs).Error; err != nil {
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

// recoverStaleTasks reclaims tasks that are marked running but no longer have
// a live goroutine in this process. It closes the durability gap left by a
// transient failure while persisting a handler result.
func (r *Runtime) recoverStaleTasks(ctx context.Context, before time.Time) error {
	r.mu.Lock()
	active := make(map[string]struct{}, len(r.active))
	for id := range r.active {
		active[id] = struct{}{}
	}
	r.mu.Unlock()

	now := time.Now()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var candidates []Task
		if err := tx.Where("status IN ? AND (heartbeat_at IS NULL OR heartbeat_at < ?)", []string{TaskRunning, TaskCancelRequested}, before).
			Order("created_at ASC, id ASC").Find(&candidates).Error; err != nil {
			return err
		}
		jobs := make(map[string]struct{})
		for _, task := range candidates {
			if _, ok := active[task.ID]; ok {
				continue
			}
			attemptStatus := AttemptInterrupted
			taskStatus := TaskQueued
			code := "worker_stale"
			message := "Worker heartbeat expired; queued again"
			finishedAt := any(nil)
			if task.CommitStartedAt != nil {
				taskStatus = TaskFailed
				code = "commit_interrupted"
				message = "The worker stopped after irreversible work began; review before retrying"
				finishedAt = &now
			} else if task.Status == TaskCancelRequested {
				attemptStatus = AttemptCanceled
				taskStatus = TaskCanceled
				code = "canceled"
				message = "Cancellation completed after the worker heartbeat expired"
				finishedAt = &now
			}
			taskUpdates := map[string]any{
				"status": taskStatus, "run_after": nil, "heartbeat_at": nil, "finished_at": finishedAt,
				"cancel_requested_at": nil, "error_code": code, "error_message": message,
			}
			if taskStatus == TaskQueued {
				taskUpdates["max_attempts"] = gorm.Expr("max_attempts + 1")
			}
			updated := tx.Model(&Task{}).
				Where("id = ? AND status = ? AND (heartbeat_at IS NULL OR heartbeat_at < ?)", task.ID, task.Status, before).
				Updates(taskUpdates)
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected == 0 {
				continue
			}
			if err := tx.Model(&Attempt{}).Where("task_id = ? AND status = ?", task.ID, AttemptRunning).Updates(map[string]any{
				"status": attemptStatus, "error_code": code, "error_message": message, "finished_at": &now,
			}).Error; err != nil {
				return err
			}
			if err := addEvent(tx, Event{JobID: task.JobID, TaskID: task.ID, Type: "task_recovered", Message: message}); err != nil {
				return err
			}
			jobs[task.JobID] = struct{}{}
		}
		for jobID := range jobs {
			var job Job
			if err := tx.First(&job, "id = ?", jobID).Error; err != nil {
				return err
			}
			if err := recomputeJob(tx, &job, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Runtime) dispatchLoop() {
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
		paused, err := r.queuePaused(queue)
		if err != nil {
			r.options.Logger.Printf("component=background event=queue_state_failed queue=%s error=%q", queue, err)
			continue
		}
		if r.rootCtx.Err() != nil || paused {
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
			Where("queue = ? AND status IN ? AND (run_after IS NULL OR run_after <= ?) AND job_id IN (SELECT id FROM background_jobs WHERE pause_requested_at IS NULL AND cancel_requested_at IS NULL)", queue, []string{TaskQueued, TaskRetryWait}, now).
			Order("priority + CAST((julianday(CURRENT_TIMESTAMP) - julianday(created_at)) * 1440 AS INTEGER) DESC").
			Order("created_at ASC, id ASC").
			First(&task).Error; err != nil {
			return err
		}
		startedAt := now
		updated := tx.Model(&Task{}).
			Where("id = ? AND status IN ? AND job_id IN (SELECT id FROM background_jobs WHERE pause_requested_at IS NULL AND cancel_requested_at IS NULL)", task.ID, []string{TaskQueued, TaskRetryWait}).
			Updates(map[string]any{
				"status":            TaskRunning,
				"attempt_count":     gorm.Expr("attempt_count + 1"),
				"started_at":        gorm.Expr("COALESCE(started_at, ?)", startedAt),
				"heartbeat_at":      &now,
				"run_after":         nil,
				"commit_started_at": nil,
				"error_code":        "",
				"error_message":     "",
				"finished_at":       nil,
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
	r.active[task.ID] = activeTask{queue: task.Queue, jobID: task.JobID, cancel: cancel}
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
		if err := r.finishWithRetry(task, attempt, result, classifyError(ctx, runErr)); err != nil {
			r.options.Logger.Printf("component=background event=finish_failed job=%s task=%s error=%q", task.JobID, task.ID, err)
		}
	}()
}

func (r *Runtime) finishWithRetry(task Task, attempt Attempt, result Result, taskErr *TaskError) error {
	var err error
	for retry := 0; retry < 5; retry++ {
		if err = r.finish(task, attempt, result, taskErr); err == nil {
			return nil
		}
		if retry < 4 {
			time.Sleep(time.Duration(retry+1) * 50 * time.Millisecond)
		}
	}
	return err
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
		if (taskErr == nil || taskErr.Class == ErrorPaused) && current.Status == TaskCancelRequested && current.CommitStartedAt == nil {
			taskErr = &TaskError{Code: "canceled", Public: "Canceled", Class: ErrorCanceled, Cause: errUserCanceled}
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
		eventMetadata := ""

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
			case ErrorPaused:
				attemptStatus = AttemptInterrupted
				taskStatus = TaskQueued
				updates["status"] = taskStatus
				updates["run_after"] = nil
				updates["finished_at"] = nil
				updates["cancel_requested_at"] = nil
				updates["max_attempts"] = gorm.Expr("max_attempts + 1")
				eventType = "task_paused"
				eventMessage = "Task paused at a safe checkpoint"
			case ErrorInterrupted:
				attemptStatus = AttemptInterrupted
				taskStatus = TaskQueued
				updates["status"] = taskStatus
				updates["run_after"] = nil
				updates["finished_at"] = nil
				updates["cancel_requested_at"] = nil
				updates["max_attempts"] = gorm.Expr("max_attempts + 1")
				eventType = "task_interrupted"
				eventMessage = "Task interrupted and queued again"
			case ErrorDeferred:
				attemptStatus = AttemptInterrupted
				taskStatus = TaskRetryWait
				delay := retryDelay(current.AttemptCount, taskErr.RetryAfter)
				runAfter := now.Add(delay)
				updates["status"] = taskStatus
				updates["run_after"] = &runAfter
				updates["finished_at"] = nil
				updates["max_attempts"] = gorm.Expr("max_attempts + 1")
				eventType = "task_deferred"
				eventMessage = fmt.Sprintf("Task deferred for %s", delay.Round(time.Second))
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
					eventMessage = fmt.Sprintf("Attempt %d failed; retry scheduled in %s", attempt.Number, delay.Round(time.Second))
				} else {
					taskStatus = TaskFailed
					updates["status"] = taskStatus
					eventType = "task_failed"
					eventMessage = fmt.Sprintf("Attempt %d failed; automatic retries exhausted", attempt.Number)
				}
			default:
				attemptStatus = AttemptFailed
				taskStatus = TaskFailed
				updates["status"] = taskStatus
				eventType = "task_failed"
				eventMessage = fmt.Sprintf("Attempt %d failed", attempt.Number)
			}
			diagnostics := RedactDiagnostic(taskErr.Diagnostic)
			if attemptStatus == AttemptFailed {
				eventMetadata = diagnostics
			}
			if err := tx.Model(&Attempt{}).Where("id = ?", attempt.ID).Updates(map[string]any{
				"status":        attemptStatus,
				"error_code":    code,
				"error_message": public,
				"diagnostics":   diagnostics,
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
		if err := addEvent(tx, Event{JobID: task.JobID, TaskID: task.ID, Type: eventType, Message: eventMessage, Metadata: eventMetadata}); err != nil {
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
	if err := tx.First(job, "id = ?", job.ID).Error; err != nil {
		return err
	}
	var tasks []Task
	if err := tx.Where("job_id = ?", job.ID).Order("created_at ASC").Find(&tasks).Error; err != nil {
		return err
	}
	if len(tasks) == 0 {
		return nil
	}
	status := JobQueued
	phase := ""
	lastPhase := ""
	var totalWeight, completedWeight int64
	running, waiting, queued, failedRequired, failedOptional, canceledRequired, canceledOptional := 0, 0, 0, 0, 0, 0, 0
	errorCode, errorMessage := "", ""
	for _, task := range tasks {
		if task.Phase != "" {
			lastPhase = task.Phase
		}
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
			if task.Required {
				canceledRequired++
			} else {
				canceledOptional++
			}
			if errorCode == "" {
				errorCode, errorMessage = task.ErrorCode, task.ErrorMessage
			}
		}
	}
	if phase == "" {
		phase = lastPhase
	}
	progress := 0
	if totalWeight > 0 {
		progress = int(completedWeight / (totalWeight / 10000))
	}
	terminal := running == 0 && waiting == 0 && queued == 0
	if !terminal {
		switch {
		case job.CancelRequestedAt != nil:
			status = JobCancelRequested
		case job.PauseRequestedAt != nil && running > 0:
			status = JobPauseRequested
		case job.PauseRequestedAt != nil:
			status = JobPaused
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
		case job.CancelRequestedAt != nil || canceledRequired > 0:
			status = JobCanceled
		case failedOptional > 0 || canceledOptional > 0:
			status = JobSucceededWithWarnings
			progress = 10000
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
	if status == JobPaused {
		updates["paused_at"] = gorm.Expr("COALESCE(paused_at, ?)", now)
	} else if status != JobPauseRequested {
		updates["paused_at"] = nil
	}
	if terminalJobStatus(status) {
		updates["pause_requested_at"] = nil
		updates["paused_at"] = nil
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
	if err := reporter.runtime.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updated := tx.Model(&Task{}).Where("id = ? AND status IN ?", reporter.taskID, []string{TaskRunning, TaskCancelRequested}).Updates(map[string]any{
			"progress": value, "progress_message": boundedMessage(message, 255), "heartbeat_at": &now,
		})
		if updated.Error != nil || updated.RowsAffected == 0 {
			return updated.Error
		}
		var task Task
		if err := tx.Select("job_id").First(&task, "id = ?", reporter.taskID).Error; err != nil {
			return err
		}
		var job Job
		if err := tx.First(&job, "id = ?", task.JobID).Error; err != nil {
			return err
		}
		return recomputeJob(tx, &job, now)
	}); err != nil && !errors.Is(err, context.Canceled) {
		reporter.runtime.options.Logger.Printf("component=background event=progress_update_failed task=%s error=%q", reporter.taskID, err)
	}
}

// BeginPhase atomically moves a running task into a new phase. Handlers use it
// as the boundary before irreversible work: cancellation either wins first
// and this returns false, or the new job phase becomes visible before an
// operator can request cancellation.
func BeginPhase(ctx context.Context, phase string) bool {
	return BeginCommit(ctx, phase)
}

// BeginCommit establishes the cancellation fence before irreversible work.
// Once it succeeds, a concurrent cancellation request is rejected and a
// successful handler result wins even if its context is canceled later.
func BeginCommit(ctx context.Context, phase string) bool {
	if ctx == nil {
		return strings.TrimSpace(phase) != ""
	}
	reporter, _ := ctx.Value(executionReporterKey{}).(*executionReporter)
	if strings.TrimSpace(phase) == "" {
		return false
	}
	// The same worker functions remain usable by legacy synchronous callers.
	// Only runtime-managed contexts need a durable cancellation fence.
	if reporter == nil {
		return ctx == nil || ctx.Err() == nil
	}
	now := time.Now()
	err := reporter.runtime.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task Task
		if err := tx.First(&task, "id = ?", reporter.taskID).Error; err != nil {
			return err
		}
		if task.Status != TaskRunning {
			return ErrConflict
		}
		var job Job
		if err := tx.First(&job, "id = ?", task.JobID).Error; err != nil {
			return err
		}
		if job.CancelRequestedAt != nil || job.PauseRequestedAt != nil {
			return ErrConflict
		}
		updated := tx.Model(&Task{}).Where("id = ? AND status = ? AND cancel_requested_at IS NULL", reporter.taskID, TaskRunning).Updates(map[string]any{"phase": boundedMessage(phase, 120), "commit_started_at": &now})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrConflict
		}
		return recomputeJob(tx, &job, now)
	})
	return err == nil
}

func (r *Runtime) PauseJob(ctx context.Context, jobID string, actorID uint, actorName string) error {
	now := time.Now()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job Job
		if err := tx.First(&job, "id = ?", jobID).Error; err != nil {
			return err
		}
		if !job.Pausable || !pausableJobStatus(job.Status) || job.PauseRequestedAt != nil || job.CancelRequestedAt != nil {
			return ErrConflict
		}
		var committing int64
		if err := tx.Model(&Task{}).Where("job_id = ? AND commit_started_at IS NOT NULL AND status IN ?", jobID, []string{TaskRunning, TaskCancelRequested}).Count(&committing).Error; err != nil {
			return err
		}
		if committing > 0 {
			return ErrCommitStarted
		}
		if err := tx.Model(&job).Updates(map[string]any{"pause_requested_at": &now, "paused_at": nil}).Error; err != nil {
			return err
		}
		job.PauseRequestedAt = &now
		if err := addEvent(tx, Event{JobID: jobID, Type: "job_pause_requested", ActorID: optionalActor(actorID), ActorName: actorName, Message: "Pause requested"}); err != nil {
			return err
		}
		return recomputeJob(tx, &job, now)
	})
	if err != nil {
		return err
	}
	r.mu.Lock()
	var cancels []context.CancelCauseFunc
	for _, active := range r.active {
		if active.jobID == jobID {
			cancels = append(cancels, active.cancel)
		}
	}
	r.mu.Unlock()
	for _, cancel := range cancels {
		cancel(errUserPaused)
	}
	return nil
}

func (r *Runtime) ResumeJob(ctx context.Context, jobID string, actorID uint, actorName string) error {
	now := time.Now()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job Job
		if err := tx.First(&job, "id = ?", jobID).Error; err != nil {
			return err
		}
		if job.Status != JobPauseRequested && job.Status != JobPaused {
			return ErrConflict
		}
		if err := tx.Model(&job).Updates(map[string]any{"pause_requested_at": nil, "paused_at": nil}).Error; err != nil {
			return err
		}
		job.PauseRequestedAt = nil
		job.PausedAt = nil
		if err := addEvent(tx, Event{JobID: jobID, Type: "job_resumed", ActorID: optionalActor(actorID), ActorName: actorName, Message: "Job resumed"}); err != nil {
			return err
		}
		return recomputeJob(tx, &job, now)
	})
	if err == nil {
		r.Wake()
	}
	return err
}

// RunJobNow releases future-scheduled queued tasks without creating a second
// job or discarding their durable history. It is intentionally limited to jobs
// that have not started; retry backoff and active work use their own controls.
func (r *Runtime) RunJobNow(ctx context.Context, jobID string, actorID uint, actorName string) error {
	now := time.Now()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job Job
		if err := tx.First(&job, "id = ?", jobID).Error; err != nil {
			return err
		}
		if job.Status != JobQueued || job.PauseRequestedAt != nil || job.CancelRequestedAt != nil {
			return ErrConflict
		}
		result := tx.Model(&Task{}).
			Where("job_id = ? AND status = ? AND run_after IS NOT NULL AND run_after > ?", jobID, TaskQueued, now).
			Update("run_after", nil)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrConflict
		}
		if err := addEvent(tx, Event{
			JobID: jobID, Type: "job_run_requested", ActorID: optionalActor(actorID), ActorName: actorName,
			Message: "Scheduled wait skipped; job released for immediate execution",
		}); err != nil {
			return err
		}
		return recomputeJob(tx, &job, now)
	})
	if err == nil {
		r.Wake()
	}
	return err
}

func (r *Runtime) heartbeatLoop() {
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
				if err := r.db.Model(&Task{}).Where("id IN ? AND status IN ?", ids, []string{TaskRunning, TaskCancelRequested}).Update("heartbeat_at", &now).Error; err != nil {
					r.options.Logger.Printf("component=background event=heartbeat_failed error=%q", err)
				}
			}
			if err := r.recoverStaleTasks(r.rootCtx, now.Add(-30*time.Second)); err != nil {
				r.options.Logger.Printf("component=background event=stale_recovery_failed error=%q", err)
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

func (r *Runtime) queuePaused(queue string) (bool, error) {
	var state QueueState
	err := r.db.First(&state, "name = ?", queue).Error
	if err != nil {
		return false, err
	}
	return state.Paused, nil
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
		dispatchable := "queue = ? AND status IN ? AND job_id IN (SELECT id FROM background_jobs WHERE pause_requested_at IS NULL AND cancel_requested_at IS NULL)"
		if err := r.db.WithContext(ctx).Model(&Task{}).Where(dispatchable, state.Name, []string{TaskQueued, TaskRetryWait}).Count(&summary.Waiting).Error; err != nil {
			return nil, err
		}
		var oldest Task
		if err := r.db.WithContext(ctx).Where(dispatchable, state.Name, []string{TaskQueued, TaskRetryWait}).Order("created_at ASC").First(&oldest).Error; err == nil {
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
		var committing int64
		if err := tx.Model(&Task{}).Where("job_id = ? AND commit_started_at IS NOT NULL AND status IN ?", jobID, []string{TaskRunning, TaskCancelRequested}).Count(&committing).Error; err != nil {
			return err
		}
		if committing > 0 {
			return ErrCommitStarted
		}
		if err := tx.Model(&job).Updates(map[string]any{"status": JobCancelRequested, "cancel_requested_at": &now, "pause_requested_at": nil, "paused_at": nil}).Error; err != nil {
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
	var ids []string
	if err := r.db.WithContext(ctx).Model(&Task{}).Where("job_id = ? AND status = ?", jobID, TaskCancelRequested).Pluck("id", &ids).Error; err != nil {
		return err
	}
	r.mu.Lock()
	var cancels []context.CancelCauseFunc
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
			"commit_started_at": nil, "progress": 0, "progress_message": "",
			"max_attempts": gorm.Expr("attempt_count + 4"),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrConflict
		}
		if err := tx.Model(&job).Updates(map[string]any{"status": JobQueued, "finished_at": nil, "cancel_requested_at": nil, "pause_requested_at": nil, "paused_at": nil, "error_code": "", "error_message": ""}).Error; err != nil {
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
		if loaded.CommitStartedAt != nil {
			return ErrCommitStarted
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
			"commit_started_at": nil, "progress": 0, "progress_message": "",
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
			if err := addEvent(r.db.WithContext(ctx), Event{JobID: jobID, Type: "feature_disabled", ActorID: optionalActor(actorID), ActorName: actorName, Message: boundedMessage(reason, 512)}); err != nil {
				return err
			}
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

func sortedKeys[K ~string, V any](values map[K]V) []K {
	keys := make([]K, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
