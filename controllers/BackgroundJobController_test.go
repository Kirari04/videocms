package controllers

import (
	"net/http/httptest"
	"testing"
	"time"

	"ch/kirari04/videocms/background"
	"github.com/labstack/echo/v4"
)

func TestAcceptedAdminJobUsesAdminLocation(t *testing.T) {
	e := echo.New()
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(httptest.NewRequest("POST", "/api/v2/admin/task-schedules/test/run", nil), recorder)
	job := &background.Job{ID: "job-1"}
	if err := acceptedBackgroundJobAt(ctx, job, "/api/v2/admin/jobs/"+job.ID); err != nil {
		t.Fatal(err)
	}
	if got := recorder.Header().Get("Location"); got != "/api/v2/admin/jobs/job-1" {
		t.Fatalf("Location = %q", got)
	}
}

func TestUserJobDetailStripsOperatorAndWorkerDiagnostics(t *testing.T) {
	actorID := uint(4)
	detail := &background.JobDetail{
		Job:    background.Job{Tasks: []background.Task{{Attempts: []background.Attempt{{Worker: "host-1", Diagnostics: "secret"}}}}},
		Events: []background.Event{{ActorID: &actorID, ActorName: "operator"}},
	}
	stripTaskDiagnostics(detail)
	attempt := detail.Tasks[0].Attempts[0]
	if attempt.Worker != "" || attempt.Diagnostics != "" || detail.Events[0].ActorID != nil || detail.Events[0].ActorName != "" {
		t.Fatalf("user detail leaked internal data: %#v %#v", attempt, detail.Events[0])
	}
}

func TestBackgroundCursorRoundTripIncludesTieBreaker(t *testing.T) {
	createdAt := time.Now().UTC().Round(0)
	encoded := encodeBackgroundCursor(background.Job{ID: "job-tie-breaker", CreatedAt: createdAt})
	decodedAt, decodedID, err := decodeBackgroundCursor(encoded)
	if err != nil || !decodedAt.Equal(createdAt) || decodedID != "job-tie-breaker" {
		t.Fatalf("cursor round trip: at=%v id=%q err=%v", decodedAt, decodedID, err)
	}
}
