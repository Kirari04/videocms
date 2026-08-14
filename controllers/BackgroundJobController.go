package controllers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ch/kirari04/videocms/background"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

type backgroundJobListResponse struct {
	Jobs       []background.Job `json:"jobs"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

type backgroundAcceptedResponse struct {
	Job               background.Job `json:"job"`
	RetryAfterSeconds int            `json:"retryAfterSeconds"`
}

func (h *Handlers) ListMyBackgroundJobs(c echo.Context) error {
	userID, ok := c.Get("UserID").(uint)
	if !ok {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "authorization_required"})
	}
	return h.listBackgroundJobs(c, background.ListFilter{OwnerID: &userID, Visibility: background.VisibilityUser})
}

func (h *Handlers) GetMyBackgroundJob(c echo.Context) error {
	userID := c.Get("UserID").(uint)
	detail, err := h.background().Job(c.Request().Context(), c.Param("id"), &userID, false)
	if err != nil {
		return backgroundAPIError(c, err)
	}
	stripTaskDiagnostics(detail)
	return c.JSON(http.StatusOK, detail)
}

func (h *Handlers) CancelMyBackgroundJob(c echo.Context) error {
	userID := c.Get("UserID").(uint)
	if _, err := h.background().Job(c.Request().Context(), c.Param("id"), &userID, false); err != nil {
		return backgroundAPIError(c, err)
	}
	username, _ := c.Get("Username").(string)
	if err := h.background().CancelJob(c.Request().Context(), c.Param("id"), userID, username); err != nil {
		return backgroundAPIError(c, err)
	}
	return c.NoContent(http.StatusAccepted)
}

func (h *Handlers) RetryMyBackgroundJob(c echo.Context) error {
	userID := c.Get("UserID").(uint)
	if _, err := h.background().Job(c.Request().Context(), c.Param("id"), &userID, false); err != nil {
		return backgroundAPIError(c, err)
	}
	username, _ := c.Get("Username").(string)
	if err := h.background().RetryJob(c.Request().Context(), c.Param("id"), userID, username); err != nil {
		return backgroundAPIError(c, err)
	}
	h.background().Wake()
	return c.NoContent(http.StatusAccepted)
}

func (h *Handlers) ListAdminBackgroundJobs(c echo.Context) error {
	return h.listBackgroundJobs(c, background.ListFilter{IncludeSystem: queryBool(c.QueryParam("includeSystem"))})
}

func (h *Handlers) GetAdminBackgroundJob(c echo.Context) error {
	detail, err := h.background().Job(c.Request().Context(), c.Param("id"), nil, true)
	if err != nil {
		return backgroundAPIError(c, err)
	}
	return c.JSON(http.StatusOK, detail)
}

func (h *Handlers) GetAdminBackgroundSummary(c echo.Context) error {
	summary, err := h.background().Summary(c.Request().Context())
	if err != nil {
		return backgroundAPIError(c, err)
	}
	return c.JSON(http.StatusOK, summary)
}

func (h *Handlers) CancelAdminBackgroundJob(c echo.Context) error {
	actorID, actorName := backgroundActor(c)
	if err := h.background().CancelJob(c.Request().Context(), c.Param("id"), actorID, actorName); err != nil {
		return backgroundAPIError(c, err)
	}
	return c.NoContent(http.StatusAccepted)
}

func (h *Handlers) RetryAdminBackgroundJob(c echo.Context) error {
	actorID, actorName := backgroundActor(c)
	if err := h.background().RetryJob(c.Request().Context(), c.Param("id"), actorID, actorName); err != nil {
		return backgroundAPIError(c, err)
	}
	h.background().Wake()
	return c.NoContent(http.StatusAccepted)
}

func (h *Handlers) CancelAdminBackgroundTask(c echo.Context) error {
	actorID, actorName := backgroundActor(c)
	if err := h.background().CancelTask(c.Request().Context(), c.Param("id"), actorID, actorName); err != nil {
		return backgroundAPIError(c, err)
	}
	return c.NoContent(http.StatusAccepted)
}

func (h *Handlers) RetryAdminBackgroundTask(c echo.Context) error {
	actorID, actorName := backgroundActor(c)
	if err := h.background().RetryTask(c.Request().Context(), c.Param("id"), actorID, actorName); err != nil {
		return backgroundAPIError(c, err)
	}
	return c.NoContent(http.StatusAccepted)
}

func (h *Handlers) ListAdminBackgroundQueues(c echo.Context) error {
	queues, err := h.background().QueueSummaries(c.Request().Context())
	if err != nil {
		return backgroundAPIError(c, err)
	}
	return c.JSON(http.StatusOK, queues)
}

func (h *Handlers) PauseAdminBackgroundQueue(c echo.Context) error {
	actorID, actorName := backgroundActor(c)
	if err := h.background().SetQueuePaused(c.Request().Context(), c.Param("name"), true, actorID, actorName); err != nil {
		return backgroundAPIError(c, err)
	}
	return c.NoContent(http.StatusOK)
}

func (h *Handlers) ResumeAdminBackgroundQueue(c echo.Context) error {
	actorID, actorName := backgroundActor(c)
	if err := h.background().SetQueuePaused(c.Request().Context(), c.Param("name"), false, actorID, actorName); err != nil {
		return backgroundAPIError(c, err)
	}
	return c.NoContent(http.StatusOK)
}

func (h *Handlers) ListAdminBackgroundSchedules(c echo.Context) error {
	schedules, err := h.background().Schedules(c.Request().Context())
	if err != nil {
		return backgroundAPIError(c, err)
	}
	return c.JSON(http.StatusOK, schedules)
}

func (h *Handlers) RunAdminBackgroundSchedule(c echo.Context) error {
	actorID, actorName := backgroundActor(c)
	job, err := h.background().RunSchedule(c.Request().Context(), c.Param("key"), actorID, actorName)
	if err != nil {
		return backgroundAPIError(c, err)
	}
	return acceptedBackgroundJob(c, job)
}

func (h *Handlers) GetAdminBackgroundRuntime(c echo.Context) error {
	queues, err := h.background().QueueSummaries(c.Request().Context())
	if err != nil {
		return backgroundAPIError(c, err)
	}
	schedules, err := h.background().Schedules(c.Request().Context())
	if err != nil {
		return backgroundAPIError(c, err)
	}
	services := []servicesHealthResponse{}
	if h.Workers != nil {
		for _, health := range h.Workers.ServiceHealth() {
			services = append(services, servicesHealthResponse{Name: health.Name, Status: health.Status, Restarts: health.Restarts, LastStartAt: health.LastStartAt, LastError: background.RedactDiagnostic(health.LastError)})
		}
	}
	if h.TUS != nil {
		for _, health := range h.TUS.ConsumerHealth() {
			services = append(services, servicesHealthResponse{Name: health.Name, Status: health.Status, Restarts: health.Restarts, LastStartAt: health.LastStartAt, LastError: background.RedactDiagnostic(health.LastError)})
		}
	}
	return c.JSON(http.StatusOK, echo.Map{"status": "running", "queues": queues, "schedules": schedules, "services": services, "checkedAt": time.Now()})
}

type servicesHealthResponse struct {
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Restarts    int        `json:"restarts"`
	LastStartAt *time.Time `json:"lastStartAt,omitempty"`
	LastError   string     `json:"lastError,omitempty"`
}

func (h *Handlers) listBackgroundJobs(c echo.Context, filter background.ListFilter) error {
	filter.Statuses = splitQuery(c.QueryParam("status"))
	filter.Kinds = splitQuery(c.QueryParam("kind"))
	filter.Queue = c.QueryParam("queue")
	filter.Search = c.QueryParam("search")
	filter.Limit, _ = strconv.Atoi(c.QueryParam("limit"))
	if filter.Limit < 1 || filter.Limit > 200 {
		filter.Limit = 50
	}
	if cursor := c.QueryParam("cursor"); cursor != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, cursor); err == nil {
			filter.Before = &parsed
		} else {
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid_cursor"})
		}
	}
	jobs, err := h.background().ListJobs(c.Request().Context(), filter)
	if err != nil {
		return backgroundAPIError(c, err)
	}
	response := backgroundJobListResponse{Jobs: jobs}
	if len(jobs) >= filter.Limit {
		response.NextCursor = jobs[len(jobs)-1].CreatedAt.Format(time.RFC3339Nano)
	}
	return c.JSON(http.StatusOK, response)
}

func acceptedBackgroundJob(c echo.Context, job *background.Job) error {
	if job == nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "job_unavailable"})
	}
	statusURL := "/api/v2/jobs/" + job.ID
	c.Response().Header().Set("Location", statusURL)
	c.Response().Header().Set("Retry-After", "2")
	return c.JSON(http.StatusAccepted, backgroundAcceptedResponse{Job: *job, RetryAfterSeconds: 2})
}

func (h *Handlers) background() *background.Runtime { return h.Deps.Background }

func backgroundActor(c echo.Context) (uint, string) {
	id, _ := c.Get("UserID").(uint)
	name, _ := c.Get("Username").(string)
	return id, name
}

func stripTaskDiagnostics(detail *background.JobDetail) {
	for taskIndex := range detail.Tasks {
		for attemptIndex := range detail.Tasks[taskIndex].Attempts {
			detail.Tasks[taskIndex].Attempts[attemptIndex].Diagnostics = ""
		}
	}
}

func splitQuery(value string) []string {
	result := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func queryBool(value string) bool {
	parsed, _ := strconv.ParseBool(value)
	return parsed
}

func backgroundAPIError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "job_not_found"})
	case errors.Is(err, background.ErrConflict):
		return c.JSON(http.StatusConflict, echo.Map{"error": "invalid_job_state"})
	default:
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "background_service_error"})
	}
}
