package controllers

import (
	"ch/kirari04/videocms/background"
	downloadsvc "ch/kirari04/videocms/download"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/middlewares"
	"ch/kirari04/videocms/models"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

type DownloadJobCreateRequest struct {
	Quality       string   `json:"quality" validate:"required,min=1,max=20"`
	Container     string   `json:"container" validate:"required,oneof=mkv mp4"`
	AudioUUIDs    []string `json:"audioUUIDs" validate:"max=100,dive,uuid_rfc4122"`
	SubtitleUUIDs []string `json:"subtitleUUIDs" validate:"max=100,dive,uuid_rfc4122"`
}

type downloadJobManifestTrack struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
	Lang string `json:"lang"`
	Type string `json:"type"`
}

type downloadJobManifest struct {
	Quality        string                     `json:"quality"`
	Container      string                     `json:"container"`
	AudioTracks    []downloadJobManifestTrack `json:"audioTracks"`
	SubtitleTracks []downloadJobManifestTrack `json:"subtitleTracks"`
}

type downloadJobFile struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"downloadUrl"`
}

type downloadJobError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type downloadJobResponse struct {
	ID                        string              `json:"id"`
	Status                    string              `json:"status"`
	Progress                  float64             `json:"progress"`
	QueuePosition             *int                `json:"queuePosition"`
	EstimatedSecondsRemaining *int                `json:"estimatedSecondsRemaining"`
	RetryAfterSeconds         int                 `json:"retryAfterSeconds"`
	Reused                    bool                `json:"reused"`
	Manifest                  downloadJobManifest `json:"manifest"`
	File                      *downloadJobFile    `json:"file"`
	Error                     *downloadJobError   `json:"error"`
	CreatedAt                 *time.Time          `json:"createdAt"`
	StartedAt                 *time.Time          `json:"startedAt"`
	ReadyAt                   *time.Time          `json:"readyAt"`
	ExpiresAt                 *time.Time          `json:"expiresAt"`
	BackgroundJobID           string              `json:"backgroundJobId,omitempty"`
}

type cachedDownloadPreparationRatio struct {
	Found bool
	Value float64
}

func (h *Handlers) CreateDownloadJob(c echo.Context) error {
	if !h.downloadsEnabled() {
		return downloadJobJSONError(c, http.StatusServiceUnavailable, "downloads_disabled", "Downloads are disabled.")
	}
	if _, ok := middlewares.MediaClaims(c); !ok {
		return downloadJobJSONError(c, http.StatusUnauthorized, "authorization_required", "Download access has expired.")
	}

	var request DownloadJobCreateRequest
	if status, err := helpers.Validate(c, &request); err != nil {
		return downloadJobJSONError(c, status, "invalid_selection", err.Error())
	}

	link, err := h.loadPlayerLink(c.Param("UUID"))
	if err != nil {
		return downloadJobJSONError(c, http.StatusNotFound, "video_not_found", "This video is no longer available.")
	}
	selection, err := downloadsvc.ResolveSelection(
		link,
		request.Quality,
		request.Container,
		false,
		true,
		request.AudioUUIDs,
		request.SubtitleUUIDs,
	)
	if err != nil {
		return downloadJobJSONError(c, http.StatusBadRequest, "invalid_selection", err.Error())
	}

	audioUUIDs := selectionAudioUUIDs(selection)
	subtitleUUIDs := selectionSubtitleUUIDs(selection)
	audioJSON, _ := json.Marshal(audioUUIDs)
	subtitleJSON, _ := json.Marshal(subtitleUUIDs)
	selectionHash := hashDownloadSelection(link, selection)
	outputName := fmt.Sprintf(
		"%s[%s].%s",
		safeDownloadName(link.Name),
		selection.Quality.Name,
		selection.Container,
	)

	var job models.DownloadJob
	reused := false
	queueFull := false
	now := time.Now()
	h.downloadJobCreateMu.Lock()
	err = h.Deps.DB.Transaction(func(tx *gorm.DB) error {
		var existing []models.DownloadJob
		if err := tx.
			Where("link_id = ? AND selection_hash = ? AND status IN ?", link.ID, selectionHash, []string{
				models.DownloadJobStatusQueued,
				models.DownloadJobStatusPreparing,
				models.DownloadJobStatusReady,
			}).
			Order("created_at DESC").
			Find(&existing).Error; err != nil {
			return err
		}
		for _, candidate := range existing {
			if candidate.Status == models.DownloadJobStatusReady {
				if candidate.ExpiresAt == nil || !candidate.ExpiresAt.After(now) {
					continue
				}
				if !downloadJobPathInside(h.Config().FolderVideoUploadsPriv, candidate.OutputPath) {
					continue
				}
				if _, err := os.Stat(candidate.OutputPath); err != nil {
					continue
				}
			}
			job = candidate
			reused = true
			return nil
		}

		var queuedCount int64
		if err := tx.Model(&models.DownloadJob{}).
			Where("status = ?", models.DownloadJobStatusQueued).
			Count(&queuedCount).Error; err != nil {
			return err
		}
		maxQueued := h.Config().MaxQueuedDownloadPreparations
		if maxQueued < 1 {
			maxQueued = 20
		}
		if queuedCount >= maxQueued {
			queueFull = true
			return nil
		}

		job = models.DownloadJob{
			UUID:          uuid.NewString(),
			LinkID:        link.ID,
			LinkUUID:      link.UUID,
			FileID:        link.FileID,
			UserID:        link.UserID,
			QualityID:     selection.Quality.ID,
			QualityName:   selection.Quality.Name,
			Container:     selection.Container,
			AudioUUIDs:    string(audioJSON),
			SubtitleUUIDs: string(subtitleJSON),
			SelectionHash: selectionHash,
			MediaDuration: link.File.Duration,
			Status:        models.DownloadJobStatusQueued,
			Progress:      0,
			OutputName:    outputName,
		}
		return tx.Create(&job).Error
	})
	h.downloadJobCreateMu.Unlock()
	if err != nil {
		return downloadJobJSONError(c, http.StatusInternalServerError, "job_create_failed", "The server could not queue this download.")
	}
	if queueFull {
		c.Response().Header().Set("Retry-After", "30")
		c.Logger().Warnf("download_preparation event=queue_full link=%s", link.UUID)
		return downloadJobJSONError(c, http.StatusTooManyRequests, "queue_full", "The download preparation queue is full. Try again shortly.")
	}
	if job.Status != models.DownloadJobStatusReady && job.BackgroundJobID == "" && h.Deps.Background != nil {
		ownerID := job.UserID
		backgroundJob, _, enqueueErr := h.background().Enqueue(c.Request().Context(), background.JobSpec{
			Kind: "download.prepare", Visibility: background.VisibilityUser, OwnerID: &ownerID,
			SubjectType: "download_preparation", SubjectID: job.UUID,
			IdempotencyKey: "prepared:" + job.UUID, Label: "Prepare " + job.OutputName,
			Tasks: []background.TaskSpec{{
				Kind: "download.prepare", Queue: background.QueueFFmpeg, Phase: "Preparing download",
				Payload: map[string]any{"downloadJobId": job.ID}, DedupeKey: fmt.Sprintf("prepared:%d", job.ID),
				Priority: 50, Required: true, Weight: 100,
			}},
		})
		if enqueueErr != nil {
			return downloadJobJSONError(c, http.StatusInternalServerError, "job_create_failed", "The server could not queue this download.")
		}
		job.BackgroundJobID = backgroundJob.ID
		if err := h.Deps.DB.Model(&job).Update("background_job_id", backgroundJob.ID).Error; err != nil {
			return downloadJobJSONError(c, http.StatusInternalServerError, "job_create_failed", "The server could not link this download.")
		}
	}

	statusURL := h.downloadJobStatusURL(job.LinkUUID, job.UUID)
	c.Response().Header().Set("Location", statusURL)
	response, err := h.buildDownloadJobResponse(&job, link, reused)
	if err != nil {
		return downloadJobJSONError(c, http.StatusInternalServerError, "job_load_failed", "The queued download could not be loaded.")
	}
	status := http.StatusAccepted
	if job.Status == models.DownloadJobStatusReady {
		status = http.StatusOK
	}
	return c.JSON(status, response)
}

func (h *Handlers) GetDownloadJob(c echo.Context) error {
	job, link, status, err := h.loadDownloadJobForRequest(c)
	if err != nil {
		return downloadJobJSONError(c, status, "job_not_found", err.Error())
	}
	if h.expireDownloadJobIfNeeded(job) {
		if err := h.Deps.DB.First(job, job.ID).Error; err != nil {
			return downloadJobJSONError(c, http.StatusNotFound, "job_not_found", "This prepared download is no longer available.")
		}
	}
	response, err := h.buildDownloadJobResponse(job, link, false)
	if err != nil {
		return downloadJobJSONError(c, http.StatusInternalServerError, "job_load_failed", "The download status could not be loaded.")
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handlers) DownloadPreparedFile(c echo.Context) error {
	if !h.downloadsEnabled() {
		return downloadJobJSONError(c, http.StatusServiceUnavailable, "downloads_disabled", "Downloads are disabled.")
	}
	job, _, status, err := h.loadDownloadJobForRequest(c)
	if err != nil {
		return downloadJobJSONError(c, status, "job_not_found", err.Error())
	}
	if h.expireDownloadJobIfNeeded(job) || job.Status == models.DownloadJobStatusExpired {
		return downloadJobJSONError(c, http.StatusGone, "job_expired", "This prepared download has expired.")
	}
	if job.Status != models.DownloadJobStatusReady {
		return downloadJobJSONError(c, http.StatusConflict, "job_not_ready", "This download is not ready yet.")
	}
	if !downloadJobPathInside(h.Config().FolderVideoUploadsPriv, job.OutputPath) {
		return downloadJobJSONError(c, http.StatusInternalServerError, "file_unavailable", "The prepared file is unavailable.")
	}

	file, err := os.Open(job.OutputPath)
	if err != nil {
		return downloadJobJSONError(c, http.StatusGone, "file_unavailable", "The prepared file is no longer available.")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return downloadJobJSONError(c, http.StatusInternalServerError, "file_unavailable", "The prepared file could not be inspected.")
	}

	contentType := "video/x-matroska"
	if job.Container == downloadsvc.ContainerMP4 {
		contentType = "video/mp4"
	}
	c.Response().Header().Set("Content-Type", contentType)
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, job.OutputName))
	c.Response().Header().Set("Accept-Ranges", "bytes")

	counter := &countingResponseWriter{ResponseWriter: c.Response().Writer}
	http.ServeContent(counter, c.Request(), job.OutputName, info.ModTime(), file)
	if counter.bytes > 0 &&
		(counter.status == http.StatusOK || counter.status == http.StatusPartialContent) {
		h.Logic.TrackDownloadTraffic(job.UserID, job.FileID, job.QualityID, counter.bytes)
	}
	return nil
}

type countingResponseWriter struct {
	http.ResponseWriter
	bytes  uint64
	status int
}

func (w *countingResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *countingResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += uint64(n)
	return n, err
}

func (h *Handlers) loadDownloadJobForRequest(c echo.Context) (*models.DownloadJob, *models.Link, int, error) {
	claims, ok := middlewares.MediaClaims(c)
	if !ok {
		return nil, nil, http.StatusUnauthorized, fmt.Errorf("download access has expired")
	}
	jobUUID := c.Param("JOBUUID")
	if _, err := uuid.Parse(jobUUID); err != nil {
		return nil, nil, http.StatusBadRequest, fmt.Errorf("invalid download job")
	}

	var job models.DownloadJob
	if err := h.Deps.DB.
		Where("uuid = ? AND link_uuid = ?", jobUUID, claims.LinkUUID).
		First(&job).Error; err != nil {
		return nil, nil, http.StatusNotFound, fmt.Errorf("this prepared download is no longer available")
	}
	var link models.Link
	if err := h.Deps.DB.
		Preload("File").
		Preload("File.Qualitys").
		Preload("File.Audios").
		Preload("File.Subtitles").
		Where("id = ? AND uuid = ?", job.LinkID, claims.LinkUUID).
		First(&link).Error; err != nil {
		return nil, nil, http.StatusNotFound, fmt.Errorf("this video is no longer available")
	}
	return &job, &link, http.StatusOK, nil
}

func (h *Handlers) buildDownloadJobResponse(
	job *models.DownloadJob,
	link *models.Link,
	reused bool,
) (*downloadJobResponse, error) {
	audioUUIDs := decodeDownloadUUIDs(job.AudioUUIDs)
	subtitleUUIDs := decodeDownloadUUIDs(job.SubtitleUUIDs)
	manifest := downloadJobManifest{
		Quality:        job.QualityName,
		Container:      job.Container,
		AudioTracks:    make([]downloadJobManifestTrack, 0, len(audioUUIDs)),
		SubtitleTracks: make([]downloadJobManifestTrack, 0, len(subtitleUUIDs)),
	}
	audioByUUID := make(map[string]models.Audio)
	for _, audio := range link.File.Audios {
		audioByUUID[audio.UUID] = audio
	}
	for _, id := range audioUUIDs {
		if audio, ok := audioByUUID[id]; ok {
			manifest.AudioTracks = append(manifest.AudioTracks, downloadJobManifestTrack{
				UUID: audio.UUID,
				Name: audio.Name,
				Lang: audio.Lang,
				Type: strings.ToUpper(audio.Codec),
			})
		}
	}
	subtitleByUUID := make(map[string]models.Subtitle)
	for _, subtitle := range link.File.Subtitles {
		subtitleByUUID[subtitle.UUID] = subtitle
	}
	for _, id := range subtitleUUIDs {
		if subtitle, ok := subtitleByUUID[id]; ok {
			manifest.SubtitleTracks = append(manifest.SubtitleTracks, downloadJobManifestTrack{
				UUID: subtitle.UUID,
				Name: subtitle.Name,
				Lang: subtitle.Lang,
				Type: strings.ToUpper(subtitle.Type),
			})
		}
	}

	response := &downloadJobResponse{
		ID:                job.UUID,
		Status:            job.Status,
		Progress:          math.Max(0, math.Min(1, job.Progress)),
		RetryAfterSeconds: 5,
		Reused:            reused,
		Manifest:          manifest,
		CreatedAt:         job.CreatedAt,
		StartedAt:         job.StartedAt,
		ReadyAt:           job.ReadyAt,
		ExpiresAt:         job.ExpiresAt,
		BackgroundJobID:   job.BackgroundJobID,
	}
	if models.IsDownloadJobTerminal(job.Status) {
		response.RetryAfterSeconds = 0
	}
	if job.Status == models.DownloadJobStatusPreparing {
		response.RetryAfterSeconds = 2
	}
	if job.Status == models.DownloadJobStatusQueued {
		position, estimate := h.downloadJobQueueEstimate(job)
		response.QueuePosition = &position
		response.EstimatedSecondsRemaining = estimate
	} else if job.Status == models.DownloadJobStatusPreparing {
		response.EstimatedSecondsRemaining = h.downloadJobPreparingEstimate(job)
	}
	if job.Status == models.DownloadJobStatusReady {
		response.File = &downloadJobFile{
			Name:        job.OutputName,
			Size:        job.OutputSize,
			DownloadURL: h.downloadJobFileURL(job.LinkUUID, job.UUID),
		}
	}
	if job.ErrorCode != "" || job.ErrorMessage != "" {
		response.Error = &downloadJobError{Code: job.ErrorCode, Message: job.ErrorMessage}
	}
	return response, nil
}

func (h *Handlers) downloadJobPreparingEstimate(job *models.DownloadJob) *int {
	if job.StartedAt != nil && time.Since(*job.StartedAt) >= 5*time.Second && job.Progress >= 0.02 {
		seconds := int(time.Since(*job.StartedAt).Seconds() / job.Progress * (1 - job.Progress))
		if seconds >= 0 {
			return &seconds
		}
	}
	ratio := h.downloadPreparationHistoryRatio()
	if ratio == nil || job.MediaDuration <= 0 {
		return nil
	}
	seconds := int(*ratio * job.MediaDuration)
	if job.StartedAt != nil {
		seconds -= int(time.Since(*job.StartedAt).Seconds())
	}
	if seconds < 0 {
		seconds = 0
	}
	return &seconds
}

func (h *Handlers) downloadJobQueueEstimate(job *models.DownloadJob) (int, *int) {
	var queued []models.DownloadJob
	_ = h.Deps.DB.
		Where("status = ?", models.DownloadJobStatusQueued).
		Order("created_at ASC, id ASC").
		Find(&queued).Error
	position := 1
	for i, queuedJob := range queued {
		if queuedJob.ID == job.ID {
			position = i + 1
			break
		}
	}

	ratio := h.downloadPreparationHistoryRatio()
	if ratio == nil {
		return position, nil
	}
	workers := int(h.Config().MaxParallelDownloadPreparations)
	if workers < 1 {
		workers = 1
	}
	var running []models.DownloadJob
	_ = h.Deps.DB.
		Where("status = ?", models.DownloadJobStatusPreparing).
		Find(&running).Error
	slots := make([]float64, workers)
	for i, runningJob := range running {
		if i >= workers {
			break
		}
		remaining := float64(0)
		if estimate := h.downloadJobPreparingEstimate(&runningJob); estimate != nil {
			remaining = float64(*estimate)
		} else {
			remaining = *ratio * runningJob.MediaDuration
		}
		slots[i] = remaining
	}
	for _, queuedJob := range queued {
		slot := 0
		for i := 1; i < len(slots); i++ {
			if slots[i] < slots[slot] {
				slot = i
			}
		}
		slots[slot] += *ratio * queuedJob.MediaDuration
		if queuedJob.ID == job.ID {
			seconds := int(slots[slot])
			return position, &seconds
		}
	}
	return position, nil
}

func (h *Handlers) downloadPreparationHistoryRatio() *float64 {
	const cacheKey = "download-preparation-history-ratio"
	if h.Deps.Cache != nil {
		if value, found := h.Deps.Cache.Get(cacheKey); found {
			cached, ok := value.(cachedDownloadPreparationRatio)
			if ok {
				if !cached.Found {
					return nil
				}
				ratio := cached.Value
				return &ratio
			}
		}
	}

	var jobs []models.DownloadJob
	if err := h.Deps.DB.
		Where("started_at IS NOT NULL AND ready_at IS NOT NULL AND media_duration > 0").
		Order("ready_at DESC").
		Limit(20).
		Find(&jobs).Error; err != nil {
		return nil
	}
	ratios := make([]float64, 0, len(jobs))
	for _, job := range jobs {
		if job.StartedAt == nil || job.ReadyAt == nil || job.MediaDuration <= 0 {
			continue
		}
		value := job.ReadyAt.Sub(*job.StartedAt).Seconds() / job.MediaDuration
		if value > 0 {
			ratios = append(ratios, value)
		}
	}
	if len(ratios) == 0 {
		if h.Deps.Cache != nil {
			h.Deps.Cache.Set(cacheKey, cachedDownloadPreparationRatio{}, time.Minute)
		}
		return nil
	}
	sort.Float64s(ratios)
	median := ratios[len(ratios)/2]
	if len(ratios)%2 == 0 {
		median = (ratios[len(ratios)/2-1] + median) / 2
	}
	if h.Deps.Cache != nil {
		h.Deps.Cache.Set(cacheKey, cachedDownloadPreparationRatio{Found: true, Value: median}, time.Minute)
	}
	return &median
}

func (h *Handlers) expireDownloadJobIfNeeded(job *models.DownloadJob) bool {
	if job.Status != models.DownloadJobStatusReady || job.ExpiresAt == nil || job.ExpiresAt.After(time.Now()) {
		return false
	}
	outputSize := job.OutputSize
	if downloadJobPathInside(h.Config().FolderVideoUploadsPriv, job.OutputPath) {
		_ = os.Remove(job.OutputPath)
	}
	now := time.Now()
	_ = h.Deps.DB.Model(job).Updates(map[string]interface{}{
		"status":      models.DownloadJobStatusExpired,
		"output_path": "",
		"output_size": 0,
		"finished_at": &now,
	}).Error
	job.Status = models.DownloadJobStatusExpired
	job.OutputPath = ""
	job.OutputSize = 0
	log.Printf("download_preparation event=expired job=%s bytes=%d", job.UUID, outputSize)
	return true
}

func hashDownloadSelection(link *models.Link, selection *downloadsvc.Selection) string {
	type versionedTrack struct {
		ID        uint
		UUID      string
		UpdatedAt *time.Time
	}
	payload := struct {
		LinkID        uint
		LinkUpdatedAt *time.Time
		FileID        uint
		FileUpdatedAt *time.Time
		Container     string
		Quality       versionedTrack
		Audios        []versionedTrack
		Subtitles     []versionedTrack
	}{
		LinkID:        link.ID,
		LinkUpdatedAt: link.UpdatedAt,
		FileID:        link.FileID,
		FileUpdatedAt: link.File.UpdatedAt,
		Container:     selection.Container,
		Quality: versionedTrack{
			ID:        selection.Quality.ID,
			UpdatedAt: selection.Quality.UpdatedAt,
		},
		Audios:    make([]versionedTrack, 0, len(selection.Audios)),
		Subtitles: make([]versionedTrack, 0, len(selection.Subtitles)),
	}
	for _, audio := range selection.Audios {
		payload.Audios = append(payload.Audios, versionedTrack{ID: audio.ID, UUID: audio.UUID, UpdatedAt: audio.UpdatedAt})
	}
	for _, subtitle := range selection.Subtitles {
		payload.Subtitles = append(payload.Subtitles, versionedTrack{ID: subtitle.ID, UUID: subtitle.UUID, UpdatedAt: subtitle.UpdatedAt})
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func selectionAudioUUIDs(selection *downloadsvc.Selection) []string {
	values := make([]string, 0, len(selection.Audios))
	for _, audio := range selection.Audios {
		values = append(values, audio.UUID)
	}
	return values
}

func selectionSubtitleUUIDs(selection *downloadsvc.Selection) []string {
	values := make([]string, 0, len(selection.Subtitles))
	for _, subtitle := range selection.Subtitles {
		values = append(values, subtitle.UUID)
	}
	return values
}

func decodeDownloadUUIDs(raw string) []string {
	values := make([]string, 0)
	_ = json.Unmarshal([]byte(raw), &values)
	return values
}

func downloadJobPathInside(uploadRoot, path string) bool {
	root, err := filepath.Abs(filepath.Join(uploadRoot, "download-jobs"))
	if err != nil {
		return false
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	return target != root && filepath.Dir(target) == root
}

func (h *Handlers) downloadJobStatusURL(linkUUID, jobUUID string) string {
	return fmt.Sprintf(
		"%s/%s/download-jobs/%s",
		strings.TrimRight(h.Config().FolderVideoQualitysPub, "/"),
		linkUUID,
		jobUUID,
	)
}

func (h *Handlers) downloadJobFileURL(linkUUID, jobUUID string) string {
	return h.downloadJobStatusURL(linkUUID, jobUUID) + "/file"
}

func downloadJobJSONError(c echo.Context, status int, code, message string) error {
	return c.JSON(status, echo.Map{
		"error": downloadJobError{
			Code:    code,
			Message: message,
		},
	})
}
