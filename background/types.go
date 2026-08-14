package background

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

const (
	JobQueued                = "queued"
	JobRunning               = "running"
	JobRetryWait             = "retry_wait"
	JobCancelRequested       = "cancel_requested"
	JobSucceeded             = "succeeded"
	JobSucceededWithWarnings = "succeeded_with_warnings"
	JobFailed                = "failed"
	JobCanceled              = "canceled"

	TaskQueued          = "queued"
	TaskRunning         = "running"
	TaskRetryWait       = "retry_wait"
	TaskCancelRequested = "cancel_requested"
	TaskSucceeded       = "succeeded"
	TaskFailed          = "failed"
	TaskCanceled        = "canceled"

	VisibilityUser   = "user"
	VisibilityAdmin  = "admin"
	VisibilitySystem = "system"

	QueueFFmpeg      = "ffmpeg"
	QueueNetwork     = "network"
	QueueStorage     = "storage"
	QueueMaintenance = "maintenance"
	QueueAudit       = "audit"

	AttemptRunning     = "running"
	AttemptSucceeded   = "succeeded"
	AttemptFailed      = "failed"
	AttemptCanceled    = "canceled"
	AttemptInterrupted = "interrupted"
)

// Job is a durable, user-visible operation. Its status and progress are
// reduced from its child tasks by the runtime.
type Job struct {
	ID                string     `gorm:"primaryKey;size:36" json:"id"`
	Kind              string     `gorm:"size:80;index" json:"kind"`
	Status            string     `gorm:"size:32;index" json:"status"`
	Visibility        string     `gorm:"size:16;index" json:"visibility"`
	OwnerID           *uint      `gorm:"index" json:"ownerId,omitempty"`
	OwnerName         string     `gorm:"-" json:"ownerName,omitempty"`
	SubjectType       string     `gorm:"size:64;index" json:"subjectType,omitempty"`
	SubjectID         string     `gorm:"size:128;index" json:"subjectId,omitempty"`
	IdempotencyKey    string     `gorm:"size:255;uniqueIndex:idx_background_job_idempotency" json:"-"`
	Label             string     `gorm:"size:255" json:"label"`
	Phase             string     `gorm:"size:120" json:"phase,omitempty"`
	Progress          int        `json:"progress"`
	ResultType        string     `gorm:"size:64" json:"resultType,omitempty"`
	ResultID          string     `gorm:"size:128" json:"resultId,omitempty"`
	ErrorCode         string     `gorm:"size:80" json:"errorCode,omitempty"`
	ErrorMessage      string     `gorm:"size:512" json:"errorMessage,omitempty"`
	CancelRequestedAt *time.Time `json:"cancelRequestedAt,omitempty"`
	StartedAt         *time.Time `json:"startedAt,omitempty"`
	FinishedAt        *time.Time `gorm:"index" json:"finishedAt,omitempty"`
	CreatedAt         time.Time  `gorm:"index" json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`

	Tasks []Task `gorm:"foreignKey:JobID" json:"tasks,omitempty"`
}

func (Job) TableName() string { return "background_jobs" }

type Task struct {
	ID                string     `gorm:"primaryKey;size:36" json:"id"`
	JobID             string     `gorm:"size:36;index;uniqueIndex:idx_background_task_dedupe" json:"jobId"`
	ParentTaskID      string     `gorm:"size:36;index" json:"parentTaskId,omitempty"`
	Kind              string     `gorm:"size:100;index" json:"kind"`
	Queue             string     `gorm:"size:32;index" json:"queue"`
	Status            string     `gorm:"size:32;index" json:"status"`
	Phase             string     `gorm:"size:120" json:"phase"`
	PayloadVersion    int        `json:"payloadVersion"`
	Payload           string     `gorm:"type:text" json:"-"`
	Result            string     `gorm:"type:text" json:"-"`
	DedupeKey         string     `gorm:"size:255;uniqueIndex:idx_background_task_dedupe" json:"dedupeKey"`
	Priority          int        `gorm:"index" json:"priority"`
	Required          bool       `json:"required"`
	Weight            int        `json:"weight"`
	Progress          int        `json:"progress"`
	ProgressMessage   string     `gorm:"size:255" json:"progressMessage,omitempty"`
	AttemptCount      int        `json:"attemptCount"`
	MaxAttempts       int        `json:"maxAttempts"`
	RunAfter          *time.Time `gorm:"index" json:"runAfter,omitempty"`
	HeartbeatAt       *time.Time `json:"heartbeatAt,omitempty"`
	ErrorCode         string     `gorm:"size:80" json:"errorCode,omitempty"`
	ErrorMessage      string     `gorm:"size:512" json:"errorMessage,omitempty"`
	CancelRequestedAt *time.Time `json:"cancelRequestedAt,omitempty"`
	StartedAt         *time.Time `json:"startedAt,omitempty"`
	FinishedAt        *time.Time `json:"finishedAt,omitempty"`
	CreatedAt         time.Time  `gorm:"index" json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`

	Attempts []Attempt `gorm:"foreignKey:TaskID" json:"attempts,omitempty"`
}

func (Task) TableName() string { return "background_tasks" }

type Attempt struct {
	ID           string     `gorm:"primaryKey;size:36" json:"id"`
	TaskID       string     `gorm:"size:36;index" json:"taskId"`
	Number       int        `json:"number"`
	Status       string     `gorm:"size:32;index" json:"status"`
	Worker       string     `gorm:"size:120" json:"worker"`
	ErrorCode    string     `gorm:"size:80" json:"errorCode,omitempty"`
	ErrorMessage string     `gorm:"size:512" json:"errorMessage,omitempty"`
	Diagnostics  string     `gorm:"type:text" json:"diagnostics,omitempty"`
	StartedAt    time.Time  `json:"startedAt"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

func (Attempt) TableName() string { return "background_task_attempts" }

type Event struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	JobID     string    `gorm:"size:36;index" json:"jobId"`
	TaskID    string    `gorm:"size:36;index" json:"taskId,omitempty"`
	Type      string    `gorm:"size:80;index" json:"type"`
	ActorID   *uint     `gorm:"index" json:"actorId,omitempty"`
	ActorName string    `gorm:"size:64" json:"actorName,omitempty"`
	Message   string    `gorm:"size:512" json:"message"`
	Metadata  string    `gorm:"type:text" json:"metadata,omitempty"`
	CreatedAt time.Time `gorm:"index" json:"createdAt"`
}

func (Event) TableName() string { return "background_task_events" }

type QueueState struct {
	Name      string     `gorm:"primaryKey;size:32" json:"name"`
	Paused    bool       `gorm:"index" json:"paused"`
	PausedBy  *uint      `json:"pausedBy,omitempty"`
	PausedAt  *time.Time `json:"pausedAt,omitempty"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

func (QueueState) TableName() string { return "background_queue_state" }

type ScheduleState struct {
	Key           string     `gorm:"primaryKey;size:100" json:"key"`
	Kind          string     `gorm:"size:100" json:"kind"`
	Queue         string     `gorm:"size:32" json:"queue"`
	Enabled       bool       `json:"enabled"`
	LastJobID     string     `gorm:"size:36" json:"lastJobId,omitempty"`
	LastStatus    string     `gorm:"size:32" json:"lastStatus,omitempty"`
	LastError     string     `gorm:"size:512" json:"lastError,omitempty"`
	LastRunAt     *time.Time `json:"lastRunAt,omitempty"`
	LastSuccessAt *time.Time `json:"lastSuccessAt,omitempty"`
	NextRunAt     *time.Time `gorm:"index" json:"nextRunAt,omitempty"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

func (ScheduleState) TableName() string { return "background_schedule_state" }

type MigrationState struct {
	Key         string `gorm:"primaryKey;size:100"`
	CompletedAt time.Time
}

func (MigrationState) TableName() string { return "background_migration_state" }

type TaskSpec struct {
	Kind           string
	Queue          string
	Phase          string
	PayloadVersion int
	Payload        any
	DedupeKey      string
	Priority       int
	Required       bool
	Weight         int
	MaxAttempts    int
	ParentTaskID   string
}

type JobSpec struct {
	Kind           string
	Visibility     string
	OwnerID        *uint
	SubjectType    string
	SubjectID      string
	IdempotencyKey string
	Label          string
	Tasks          []TaskSpec
}

type Result struct {
	Value      any
	ResultType string
	ResultID   string
	Phase      string
	Children   []TaskSpec
}

type Handler func(context.Context, Task) (Result, error)

type JobDetail struct {
	Job
	Events []Event `json:"events"`
}

type ListFilter struct {
	OwnerID       *uint
	IncludeSystem bool
	Visibility    string
	Statuses      []string
	Kinds         []string
	Queue         string
	Search        string
	Limit         int
	Before        *time.Time
}

type QueueSummary struct {
	QueueState
	Capacity int        `json:"capacity"`
	Active   int        `json:"active"`
	Waiting  int64      `json:"waiting"`
	OldestAt *time.Time `json:"oldestAt,omitempty"`
}

type Summary struct {
	Running      int64 `json:"running"`
	Waiting      int64 `json:"waiting"`
	Failed24h    int64 `json:"failed24h"`
	PausedQueues int64 `json:"pausedQueues"`
}

type ScheduleDefinition struct {
	Key        string
	Kind       string
	Queue      string
	Interval   time.Duration
	RunOnStart bool
	Build      func() JobSpec
}

func terminalJobStatus(status string) bool {
	switch status {
	case JobSucceeded, JobSucceededWithWarnings, JobFailed, JobCanceled:
		return true
	default:
		return false
	}
}

func terminalTaskStatus(status string) bool {
	switch status {
	case TaskSucceeded, TaskFailed, TaskCanceled:
		return true
	default:
		return false
	}
}

func marshalPayload(value any) (string, error) {
	if value == nil {
		return "{}", nil
	}
	data, err := json.Marshal(value)
	return string(data), err
}

func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&Job{}, &Task{}, &Attempt{}, &Event{}, &QueueState{}, &ScheduleState{}, &MigrationState{})
}
