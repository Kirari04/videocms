package services

import (
	"ch/kirari04/videocms/models"
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

func (w *WorkerGroup) encoderLogf(event, format string, args ...interface{}) {
	logger := w.encoderLogger
	if logger == nil {
		logger = log.Default()
	}
	message := fmt.Sprintf("component=encoder event=%s", event)
	if format != "" {
		message += " " + fmt.Sprintf(format, args...)
	}
	logger.Print(message)
}

func (w *WorkerGroup) encodingTaskLog(event string, task EncodingTask, started time.Time, taskErr error) {
	details := fmt.Sprintf(
		"task_type=%s file_id=%d file_uuid=%q task_id=%d task_name=%q storage_id=%q",
		task.Type,
		task.FileID,
		task.FileUUID,
		task.ID,
		task.Name,
		task.StorageID,
	)
	if !started.IsZero() {
		details += fmt.Sprintf(" duration_ms=%d", time.Since(started).Milliseconds())
	}
	if taskErr != nil {
		details += fmt.Sprintf(" error=%q", taskErr.Error())
	}
	w.encoderLogf(event, "%s", details)
}

func (w *WorkerGroup) failEncodingTask(task IwithProcess, cause error) error {
	if cause == nil {
		cause = fmt.Errorf("encoding failed without an error")
	}
	message := cause.Error()
	const maxStoredEncodingError = 8192
	if len(message) > maxStoredEncodingError {
		message = message[len(message)-maxStoredEncodingError:]
	}

	switch value := task.(type) {
	case *models.Quality:
		value.Ready = false
		value.Encoding = false
		value.Failed = true
		value.Error = message
	case *models.Audio:
		value.Ready = false
		value.Encoding = false
		value.Failed = true
		value.Error = message
	case *models.Subtitle:
		value.Ready = false
		value.Encoding = false
		value.Failed = true
		value.Error = message
	default:
		return fmt.Errorf("%w; unsupported task model %T", cause, task)
	}

	if result := task.Save(w.deps.DB); result.Error != nil {
		return fmt.Errorf("%w; persist failed state: %v", cause, result.Error)
	}
	return cause
}

func (w *WorkerGroup) completeEncodingTask(task IwithProcess) error {
	switch value := task.(type) {
	case *models.Quality:
		value.Encoding = false
		value.Failed = false
		value.Ready = true
		value.Error = ""
	case *models.Audio:
		value.Encoding = false
		value.Failed = false
		value.Ready = true
		value.Error = ""
	case *models.Subtitle:
		value.Encoding = false
		value.Failed = false
		value.Ready = true
		value.Error = ""
	default:
		return fmt.Errorf("unsupported task model %T", task)
	}

	if result := task.Save(w.deps.DB); result.Error != nil {
		return fmt.Errorf("persist completed state: %w", result.Error)
	}
	return nil
}

func (w *WorkerGroup) cleanupEncodingResource(task EncodingTask, resource string, cleanup func() error) {
	if cleanup == nil {
		return
	}
	if err := cleanup(); err != nil {
		w.encodingTaskLog(
			"cleanup_failed",
			task,
			time.Time{},
			fmt.Errorf("cleanup %s: %w", resource, err),
		)
	}
}

type encodingDiagnosticTail struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func newEncodingDiagnosticTail(limit int) *encodingDiagnosticTail {
	return &encodingDiagnosticTail{limit: limit}
}

func (b *encodingDiagnosticTail) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	written := len(p)
	if b.limit <= 0 {
		return written, nil
	}
	if len(p) >= b.limit {
		b.data = append(b.data[:0], p[len(p)-b.limit:]...)
		return written, nil
	}
	if overflow := len(b.data) + len(p) - b.limit; overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:len(b.data)-overflow]
	}
	b.data = append(b.data, p...)
	return written, nil
}

func (b *encodingDiagnosticTail) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.data))
}

func ffmpegEncodingError(ctx context.Context, runErr error, diagnostics *encodingDiagnosticTail) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("ffmpeg canceled: %w", ctxErr)
	}
	message := diagnostics.String()
	if message == "" {
		return fmt.Errorf("ffmpeg failed: %w", runErr)
	}
	return fmt.Errorf("ffmpeg failed: %w; diagnostic tail: %s", runErr, message)
}
