package background

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (r *Runtime) Enqueue(ctx context.Context, spec JobSpec) (*Job, bool, error) {
	if err := validateJobSpec(spec); err != nil {
		return nil, false, err
	}
	var job Job
	reused := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if spec.IdempotencyKey != "" {
			err := tx.Where("idempotency_key = ?", spec.IdempotencyKey).First(&job).Error
			if err == nil {
				reused = true
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		visibility := spec.Visibility
		if visibility == "" {
			visibility = VisibilityUser
		}
		job = Job{
			ID:             uuid.NewString(),
			Kind:           spec.Kind,
			Status:         JobQueued,
			Visibility:     visibility,
			OwnerID:        spec.OwnerID,
			SubjectType:    boundedMessage(spec.SubjectType, 64),
			SubjectID:      boundedMessage(spec.SubjectID, 128),
			IdempotencyKey: boundedMessage(spec.IdempotencyKey, 255),
			Label:          boundedMessage(spec.Label, 255),
			Progress:       0,
		}
		if err := tx.Create(&job).Error; err != nil {
			if spec.IdempotencyKey != "" && strings.Contains(strings.ToLower(err.Error()), "unique") {
				if loadErr := tx.Where("idempotency_key = ?", spec.IdempotencyKey).First(&job).Error; loadErr == nil {
					reused = true
					return nil
				}
			}
			return err
		}
		for index, taskSpec := range spec.Tasks {
			if taskSpec.DedupeKey == "" {
				taskSpec.DedupeKey = fmt.Sprintf("%s:%d", taskSpec.Kind, index)
			}
			if _, err := createTask(tx, job.ID, taskSpec); err != nil {
				return err
			}
		}
		return addEvent(tx, Event{JobID: job.ID, Type: "job_created", Message: "Background job queued"})
	})
	if err != nil {
		return nil, false, err
	}
	r.Wake()
	return &job, reused, nil
}

func validateJobSpec(spec JobSpec) error {
	if strings.TrimSpace(spec.Kind) == "" {
		return errors.New("background job kind is required")
	}
	if len(spec.Tasks) == 0 {
		return errors.New("background job requires at least one task")
	}
	for _, task := range spec.Tasks {
		if strings.TrimSpace(task.Kind) == "" || strings.TrimSpace(task.Queue) == "" {
			return errors.New("background task kind and queue are required")
		}
	}
	return nil
}

func createTask(tx *gorm.DB, jobID string, spec TaskSpec) (*Task, error) {
	payload, err := marshalPayload(spec.Payload)
	if err != nil {
		return nil, fmt.Errorf("encode task payload: %w", err)
	}
	if spec.PayloadVersion < 1 {
		spec.PayloadVersion = 1
	}
	if spec.Weight < 1 {
		spec.Weight = 1
	}
	if spec.MaxAttempts < 1 {
		spec.MaxAttempts = 4
	}
	task := &Task{
		ID:             uuid.NewString(),
		JobID:          jobID,
		ParentTaskID:   spec.ParentTaskID,
		Kind:           spec.Kind,
		Queue:          spec.Queue,
		Status:         TaskQueued,
		Phase:          spec.Phase,
		PayloadVersion: spec.PayloadVersion,
		Payload:        payload,
		DedupeKey:      boundedMessage(spec.DedupeKey, 255),
		Priority:       spec.Priority,
		Required:       spec.Required,
		Weight:         spec.Weight,
		MaxAttempts:    spec.MaxAttempts,
	}
	if err := tx.Create(task).Error; err != nil {
		return nil, err
	}
	if err := addEvent(tx, Event{JobID: jobID, TaskID: task.ID, Type: "task_created", Message: "Task queued"}); err != nil {
		return nil, err
	}
	return task, nil
}

func addEvent(tx *gorm.DB, event Event) error {
	event.Message = boundedMessage(event.Message, 512)
	event.Metadata = RedactDiagnostic(boundedMessage(event.Metadata, 2048))
	return tx.Create(&event).Error
}

func (r *Runtime) Job(ctx context.Context, id string, ownerID *uint, admin bool) (*JobDetail, error) {
	var job Job
	query := r.db.WithContext(ctx).Where("id = ?", id)
	if !admin {
		if ownerID == nil {
			return nil, gorm.ErrRecordNotFound
		}
		query = query.Where("owner_id = ?", *ownerID)
	}
	if err := query.First(&job).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).
		Where("job_id = ?", job.ID).
		Order("created_at ASC").
		Preload("Attempts", func(db *gorm.DB) *gorm.DB { return db.Order("number ASC") }).
		Find(&job.Tasks).Error; err != nil {
		return nil, err
	}
	var events []Event
	if err := r.db.WithContext(ctx).Where("job_id = ?", job.ID).Order("created_at ASC, id ASC").Find(&events).Error; err != nil {
		return nil, err
	}
	return &JobDetail{Job: job, Events: events}, nil
}

func (r *Runtime) ListJobs(ctx context.Context, filter ListFilter) ([]Job, error) {
	limit := filter.Limit
	if limit < 1 || limit > 200 {
		limit = 50
	}
	query := r.db.WithContext(ctx).Model(&Job{})
	if filter.OwnerID != nil {
		query = query.Where("owner_id = ?", *filter.OwnerID)
	}
	if filter.Visibility != "" {
		query = query.Where("visibility = ?", filter.Visibility)
	}
	if !filter.IncludeSystem {
		query = query.Where("visibility <> ? OR status NOT IN ?", VisibilitySystem, []string{JobSucceeded, JobSucceededWithWarnings})
	}
	if len(filter.Statuses) > 0 {
		query = query.Where("status IN ?", filter.Statuses)
	}
	if len(filter.Kinds) > 0 {
		query = query.Where("kind IN ?", filter.Kinds)
	}
	if filter.Queue != "" {
		query = query.Where("EXISTS (SELECT 1 FROM background_tasks bt WHERE bt.job_id = background_jobs.id AND bt.queue = ?)", filter.Queue)
	}
	if filter.Search != "" {
		needle := "%" + strings.ToLower(strings.TrimSpace(filter.Search)) + "%"
		query = query.Where("LOWER(label) LIKE ? OR LOWER(subject_id) LIKE ? OR LOWER(id) LIKE ?", needle, needle, needle)
	}
	if filter.Before != nil {
		query = query.Where("created_at < ?", *filter.Before)
	}
	var jobs []Job
	if err := query.Order("created_at DESC, id DESC").Limit(limit).Find(&jobs).Error; err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return jobs, nil
	}
	ownerIDs := make([]uint, 0)
	for _, job := range jobs {
		if job.OwnerID != nil {
			ownerIDs = append(ownerIDs, *job.OwnerID)
		}
	}
	if len(ownerIDs) > 0 {
		var users []struct {
			ID       uint
			Username string
		}
		_ = r.db.WithContext(ctx).Table("users").Select("id", "username").Where("id IN ?", ownerIDs).Find(&users).Error
		byID := make(map[uint]string, len(users))
		for _, user := range users {
			byID[user.ID] = user.Username
		}
		for index := range jobs {
			if jobs[index].OwnerID != nil {
				jobs[index].OwnerName = byID[*jobs[index].OwnerID]
			}
		}
	}
	return jobs, nil
}

func (r *Runtime) Summary(ctx context.Context) (Summary, error) {
	var result Summary
	if err := r.db.WithContext(ctx).Model(&Job{}).Where("status = ?", JobRunning).Count(&result.Running).Error; err != nil {
		return result, err
	}
	if err := r.db.WithContext(ctx).Model(&Job{}).Where("status IN ?", []string{JobQueued, JobRetryWait, JobCancelRequested}).Count(&result.Waiting).Error; err != nil {
		return result, err
	}
	if err := r.db.WithContext(ctx).Model(&Job{}).Where("status = ? AND finished_at >= ?", JobFailed, time.Now().Add(-24*time.Hour)).Count(&result.Failed24h).Error; err != nil {
		return result, err
	}
	if err := r.db.WithContext(ctx).Model(&QueueState{}).Where("paused = ?", true).Count(&result.PausedQueues).Error; err != nil {
		return result, err
	}
	return result, nil
}

func decodeResult(task Task, target any) error {
	if task.Result == "" {
		return nil
	}
	return json.Unmarshal([]byte(task.Result), target)
}
