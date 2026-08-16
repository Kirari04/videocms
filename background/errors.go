package background

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type ErrorClass string

const (
	ErrorTransient   ErrorClass = "transient"
	ErrorPermanent   ErrorClass = "permanent"
	ErrorCanceled    ErrorClass = "canceled"
	ErrorInterrupted ErrorClass = "interrupted"
	ErrorPaused      ErrorClass = "paused"
)

type TaskError struct {
	Code       string
	Public     string
	Diagnostic string
	Class      ErrorClass
	RetryAfter time.Duration
	Cause      error
}

func (e *TaskError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Public, e.Cause)
	}
	return e.Public
}

func (e *TaskError) Unwrap() error { return e.Cause }

func Permanent(code, public string, cause error) error {
	return &TaskError{Code: code, Public: public, Diagnostic: diagnostic(cause), Class: ErrorPermanent, Cause: cause}
}

func Transient(code, public string, cause error) error {
	return &TaskError{Code: code, Public: public, Diagnostic: diagnostic(cause), Class: ErrorTransient, Cause: cause}
}

func Canceled(cause error) error {
	return &TaskError{Code: "canceled", Public: "Canceled", Diagnostic: diagnostic(cause), Class: ErrorCanceled, Cause: cause}
}

func Interrupted(cause error) error {
	return &TaskError{Code: "interrupted", Public: "Interrupted by application shutdown", Diagnostic: diagnostic(cause), Class: ErrorInterrupted, Cause: cause}
}

func Paused(cause error) error {
	return &TaskError{Code: "paused", Public: "Paused", Diagnostic: diagnostic(cause), Class: ErrorPaused, Cause: cause}
}

func PauseRequested(ctx context.Context) bool {
	return ctx != nil && errors.Is(context.Cause(ctx), errUserPaused)
}

func classifyError(ctx context.Context, err error) *TaskError {
	if err == nil {
		return nil
	}
	var taskErr *TaskError
	if errors.As(err, &taskErr) {
		return taskErr
	}
	if errors.Is(err, context.Canceled) {
		if ctx != nil && errors.Is(context.Cause(ctx), errRuntimeStopping) {
			return &TaskError{Code: "interrupted", Public: "Interrupted by application shutdown", Diagnostic: diagnostic(err), Class: ErrorInterrupted, Cause: err}
		}
		if ctx != nil && errors.Is(context.Cause(ctx), errUserPaused) {
			return &TaskError{Code: "paused", Public: "Paused", Diagnostic: diagnostic(err), Class: ErrorPaused, Cause: err}
		}
		return &TaskError{Code: "canceled", Public: "Canceled", Diagnostic: diagnostic(err), Class: ErrorCanceled, Cause: err}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &TaskError{Code: "timeout", Public: "The operation timed out", Diagnostic: diagnostic(err), Class: ErrorTransient, Cause: err}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		return &TaskError{Code: "network_error", Public: "A temporary network error occurred", Diagnostic: diagnostic(err), Class: ErrorTransient, Cause: err}
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "database is locked") || strings.Contains(message, "database is busy") {
		return &TaskError{Code: "database_busy", Public: "The database is temporarily busy", Diagnostic: diagnostic(err), Class: ErrorTransient, Cause: err}
	}
	return &TaskError{Code: "task_failed", Public: "The background task failed", Diagnostic: diagnostic(err), Class: ErrorPermanent, Cause: err}
}

const maxDiagnosticBytes = 8192

var (
	urlPattern    = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s"']+`)
	secretPattern = regexp.MustCompile(`(?i)((?:authorization|api[_-]?key|token|secret|password|credential)["']?\s*[:=]\s*["']?)(?:bearer\s+)?([^"'\s,;}]+)`)
)

func diagnostic(err error) string {
	if err == nil {
		return ""
	}
	return RedactDiagnostic(err.Error())
}

func RedactDiagnostic(value string) string {
	value = urlPattern.ReplaceAllStringFunc(value, func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "[redacted-url]"
		}
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String()
	})
	value = secretPattern.ReplaceAllString(value, "$1[redacted]")
	if len(value) > maxDiagnosticBytes {
		value = value[len(value)-maxDiagnosticBytes:]
	}
	return strings.TrimSpace(value)
}

func boundedMessage(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit > 0 && len(value) > limit {
		return value[:limit]
	}
	return value
}
