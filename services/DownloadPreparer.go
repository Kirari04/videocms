package services

import (
	downloadsvc "ch/kirari04/videocms/download"
	"ch/kirari04/videocms/models"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type activeDownloadPreparation struct {
	jobUUID string
	fileID  uint
	cancel  context.CancelFunc
}

func (w *WorkerGroup) DownloadPreparer(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := os.MkdirAll(w.downloadJobDirectory(), 0o700); err != nil {
		log.Printf("download_preparation event=startup_failed error=%q", err.Error())
		return
	}
	w.recoverDownloadPreparations()
	w.runDownloadPreparationCleanup()
	if !w.downloadPreparationsEnabled() {
		w.CancelAllDownloadPreparations("Downloads disabled by administrator")
	}

	for {
		if w.downloadPreparationsEnabled() {
			w.loadDownloadPreparationTasks(ctx)
		}
		if !sleepContext(ctx, time.Second) {
			return
		}
	}
}

func (w *WorkerGroup) DownloadPreparationCleanup(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	for sleepContext(ctx, time.Minute) {
		w.runDownloadPreparationCleanup()
	}
}

func (w *WorkerGroup) downloadJobDirectory() string {
	return filepath.Join(w.Config().FolderVideoUploadsPriv, "download-jobs")
}

func (w *WorkerGroup) downloadPreparationsEnabled() bool {
	cfg := w.Config()
	return cfg.DownloadEnabled != nil && *cfg.DownloadEnabled
}

func (w *WorkerGroup) maxParallelDownloadPreparations() int {
	value := int(w.Config().MaxParallelDownloadPreparations)
	if value < 1 {
		return 1
	}
	if value > 8 {
		return 8
	}
	return value
}

func (w *WorkerGroup) availableDownloadPreparationSlots() int {
	w.activePreparationsMu.Lock()
	active := len(w.activePreparations)
	w.activePreparationsMu.Unlock()
	return w.maxParallelDownloadPreparations() - active
}

func (w *WorkerGroup) loadDownloadPreparationTasks(ctx context.Context) {
	slots := w.availableDownloadPreparationSlots()
	if slots <= 0 {
		return
	}

	var jobs []models.DownloadJob
	if err := w.deps.DB.
		Where("status = ?", models.DownloadJobStatusQueued).
		Order("created_at ASC, id ASC").
		Limit(slots).
		Find(&jobs).Error; err != nil {
		log.Printf("download_preparation event=load_failed error=%q", err.Error())
		return
	}

	for _, job := range jobs {
		if ctx.Err() != nil || !w.downloadPreparationsEnabled() || w.availableDownloadPreparationSlots() <= 0 {
			return
		}
		now := time.Now()
		claimed := w.deps.DB.Model(&models.DownloadJob{}).
			Where("id = ? AND status = ?", job.ID, models.DownloadJobStatusQueued).
			Updates(map[string]interface{}{
				"status":        models.DownloadJobStatusPreparing,
				"started_at":    &now,
				"finished_at":   nil,
				"progress":      0,
				"error_code":    "",
				"error_message": "",
			})
		if claimed.Error != nil || claimed.RowsAffected != 1 {
			continue
		}

		job.Status = models.DownloadJobStatusPreparing
		job.StartedAt = &now
		jobCtx, cancel := context.WithCancel(ctx)
		w.registerDownloadPreparation(job, cancel)
		log.Printf("download_preparation event=claimed job=%s file_id=%d", job.UUID, job.FileID)
		go func(task models.DownloadJob, taskCtx context.Context, stop context.CancelFunc) {
			defer stop()
			defer w.unregisterDownloadPreparation(task.ID)
			w.processDownloadPreparation(taskCtx, task)
		}(job, jobCtx, cancel)
	}
}

func (w *WorkerGroup) registerDownloadPreparation(job models.DownloadJob, cancel context.CancelFunc) {
	w.activePreparationsMu.Lock()
	w.activePreparations[job.ID] = activeDownloadPreparation{
		jobUUID: job.UUID,
		fileID:  job.FileID,
		cancel:  cancel,
	}
	w.activePreparationsMu.Unlock()
}

func (w *WorkerGroup) unregisterDownloadPreparation(id uint) {
	w.activePreparationsMu.Lock()
	delete(w.activePreparations, id)
	w.activePreparationsMu.Unlock()
}

func (w *WorkerGroup) processDownloadPreparation(parentCtx context.Context, job models.DownloadJob) {
	var link models.Link
	if err := w.deps.DB.
		Preload("File").
		Preload("File.Qualitys").
		Preload("File.Audios").
		Preload("File.Subtitles").
		Where("id = ? AND uuid = ?", job.LinkID, job.LinkUUID).
		First(&link).Error; err != nil {
		w.cancelDownloadJob(&job, "source_unavailable", "This video is no longer available.")
		return
	}

	var audioUUIDs []string
	var subtitleUUIDs []string
	if err := json.Unmarshal([]byte(job.AudioUUIDs), &audioUUIDs); err != nil {
		w.failDownloadJob(&job, "invalid_selection", "The saved audio selection is invalid.", err)
		return
	}
	if err := json.Unmarshal([]byte(job.SubtitleUUIDs), &subtitleUUIDs); err != nil {
		w.failDownloadJob(&job, "invalid_selection", "The saved subtitle selection is invalid.", err)
		return
	}

	selection, err := downloadsvc.ResolveSelection(
		&link,
		job.QualityName,
		job.Container,
		false,
		true,
		audioUUIDs,
		subtitleUUIDs,
	)
	if err != nil || selection.Quality.ID != job.QualityID {
		if err == nil {
			err = errors.New("selected quality changed")
		}
		w.failDownloadJob(&job, "source_changed", "The selected tracks are no longer available.", err)
		return
	}

	dir := w.downloadJobDirectory()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		w.failDownloadJob(&job, "storage_unavailable", "The server could not prepare storage for this download.", err)
		return
	}
	partPath := filepath.Join(dir, job.UUID+"."+job.Container+".part")
	finalPath := filepath.Join(dir, job.UUID+"."+job.Container)
	_ = os.Remove(partPath)
	_ = os.Remove(finalPath)
	defer os.Remove(partPath)

	timeout := w.preparationTimeout(job.MediaDuration)
	jobCtx, cancelTimeout := context.WithTimeout(parentCtx, timeout)
	defer cancelTimeout()
	cleanupInputs, err := downloadsvc.MaterializeSelection(jobCtx, w.deps.Storage, &link.File, selection)
	if err != nil {
		w.failDownloadJob(&job, "source_unavailable", "The selected tracks could not be prepared.", err)
		return
	}
	defer cleanupInputs()

	var progressMu sync.Mutex
	lastProgress := float64(0)
	lastUpdate := time.Time{}
	progressFn := func(value float64) {
		if value >= 1 {
			value = 0.99
		}
		if value < 0 {
			value = 0
		}
		progressMu.Lock()
		defer progressMu.Unlock()
		if time.Since(lastUpdate) < time.Second || value-lastProgress < 0.005 {
			return
		}
		lastProgress = value
		lastUpdate = time.Now()
		_ = w.deps.DB.Model(&models.DownloadJob{}).
			Where("id = ? AND status = ?", job.ID, models.DownloadJobStatusPreparing).
			Update("progress", value).Error
	}

	started := time.Now()
	err = w.downloadAssembler.Assemble(jobCtx, selection, partPath, progressFn)
	if err != nil {
		if parentCtx.Err() != nil {
			return
		}
		var current models.DownloadJob
		if loadErr := w.deps.DB.First(&current, job.ID).Error; loadErr == nil &&
			current.Status == models.DownloadJobStatusCanceled {
			return
		}
		if errors.Is(jobCtx.Err(), context.DeadlineExceeded) {
			w.failDownloadJob(&job, "preparation_timeout", "Preparing this download took too long.", err)
			return
		}
		w.failDownloadJob(&job, "preparation_failed", "The server could not prepare this download.", err)
		return
	}

	if err := os.Rename(partPath, finalPath); err != nil {
		w.failDownloadJob(&job, "storage_unavailable", "The server could not finalize this download.", err)
		return
	}
	info, err := os.Stat(finalPath)
	if err != nil {
		_ = os.Remove(finalPath)
		w.failDownloadJob(&job, "storage_unavailable", "The server could not inspect the prepared file.", err)
		return
	}

	now := time.Now()
	retentionHours := w.Config().DownloadPreparationRetentionHours
	if retentionHours < 1 {
		retentionHours = 6
	}
	expires := now.Add(time.Duration(retentionHours) * time.Hour)
	updated := w.deps.DB.Model(&models.DownloadJob{}).
		Where("id = ? AND status = ?", job.ID, models.DownloadJobStatusPreparing).
		Updates(map[string]interface{}{
			"status":        models.DownloadJobStatusReady,
			"progress":      1,
			"output_path":   finalPath,
			"output_size":   info.Size(),
			"ready_at":      &now,
			"finished_at":   &now,
			"expires_at":    &expires,
			"error_code":    "",
			"error_message": "",
		})
	if updated.Error != nil || updated.RowsAffected != 1 {
		_ = os.Remove(finalPath)
		return
	}
	log.Printf(
		"download_preparation event=ready job=%s bytes=%d duration_seconds=%.2f",
		job.UUID,
		info.Size(),
		time.Since(started).Seconds(),
	)
}

func downloadPreparationTimeout(mediaDuration float64) time.Duration {
	if mediaDuration <= 0 {
		return 6 * time.Hour
	}
	timeout := time.Duration(mediaDuration*2) * time.Second
	if timeout < 30*time.Minute {
		timeout = 30 * time.Minute
	}
	if timeout > 24*time.Hour {
		timeout = 24 * time.Hour
	}
	return timeout
}

func (w *WorkerGroup) failDownloadJob(job *models.DownloadJob, code, message string, cause error) {
	now := time.Now()
	_ = w.deps.DB.Model(&models.DownloadJob{}).
		Where("id = ? AND status = ?", job.ID, models.DownloadJobStatusPreparing).
		Updates(map[string]interface{}{
			"status":        models.DownloadJobStatusFailed,
			"progress":      0,
			"error_code":    code,
			"error_message": message,
			"finished_at":   &now,
		}).Error
	log.Printf("download_preparation event=failed job=%s code=%s error=%q", job.UUID, code, cause.Error())
}

func (w *WorkerGroup) cancelDownloadJob(job *models.DownloadJob, code, message string) {
	now := time.Now()
	_ = w.deps.DB.Model(&models.DownloadJob{}).
		Where("id = ? AND status IN ?", job.ID, []string{
			models.DownloadJobStatusQueued,
			models.DownloadJobStatusPreparing,
			models.DownloadJobStatusReady,
		}).
		Updates(map[string]interface{}{
			"status":        models.DownloadJobStatusCanceled,
			"progress":      0,
			"error_code":    code,
			"error_message": message,
			"output_path":   "",
			"output_size":   0,
			"finished_at":   &now,
			"expires_at":    nil,
		}).Error
	if job.OutputPath != "" {
		w.removeDownloadJobPath(job.OutputPath)
	}
	w.removeDownloadJobPath(filepath.Join(w.downloadJobDirectory(), job.UUID+"."+job.Container+".part"))
	log.Printf("download_preparation event=canceled job=%s code=%s", job.UUID, code)
}

func (w *WorkerGroup) recoverDownloadPreparations() {
	dir := w.downloadJobDirectory()
	entries, _ := os.ReadDir(dir)
	var staleParts int
	var staleBytes int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".part") {
			continue
		}
		var size int64
		if info, err := entry.Info(); err == nil {
			size = info.Size()
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err == nil {
			staleParts++
			staleBytes += size
		}
	}

	reset := w.deps.DB.Model(&models.DownloadJob{}).
		Where("status = ?", models.DownloadJobStatusPreparing).
		Updates(map[string]interface{}{
			"status":     models.DownloadJobStatusQueued,
			"progress":   0,
			"started_at": nil,
		})

	var ready []models.DownloadJob
	_ = w.deps.DB.Where("status = ?", models.DownloadJobStatusReady).Find(&ready).Error
	var missing int
	for _, job := range ready {
		if w.downloadJobPathInside(job.OutputPath) {
			if _, err := os.Stat(job.OutputPath); err == nil {
				continue
			}
		}
		now := time.Now()
		_ = w.deps.DB.Model(&job).Updates(map[string]interface{}{
			"status":      models.DownloadJobStatusExpired,
			"output_path": "",
			"output_size": 0,
			"finished_at": &now,
		}).Error
		missing++
	}
	log.Printf(
		"download_preparation event=recovered reset=%d stale_parts=%d stale_bytes=%d missing_ready=%d",
		reset.RowsAffected,
		staleParts,
		staleBytes,
		missing,
	)
}

func (w *WorkerGroup) downloadJobPathInside(path string) bool {
	if path == "" {
		return false
	}
	dirAbs, err := filepath.Abs(w.downloadJobDirectory())
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	return pathAbs != dirAbs && filepath.Dir(pathAbs) == dirAbs
}

func (w *WorkerGroup) runDownloadPreparationCleanup() {
	now := time.Now()
	var expired []models.DownloadJob
	_ = w.deps.DB.
		Where("status = ? AND expires_at IS NOT NULL AND expires_at <= ?", models.DownloadJobStatusReady, now).
		Find(&expired).Error
	var removedBytes int64
	var removedFiles int
	for _, job := range expired {
		bytes, removed := w.removeDownloadJobPath(job.OutputPath)
		if removed {
			removedFiles++
			removedBytes += bytes
		}
		_ = w.deps.DB.Model(&job).Updates(map[string]interface{}{
			"status":      models.DownloadJobStatusExpired,
			"output_path": "",
			"output_size": 0,
			"finished_at": &now,
		}).Error
		log.Printf("download_preparation event=expired job=%s bytes=%d", job.UUID, job.OutputSize)
	}

	var invalid []models.DownloadJob
	_ = w.deps.DB.
		Where("status IN ?", []string{
			models.DownloadJobStatusQueued,
			models.DownloadJobStatusPreparing,
			models.DownloadJobStatusReady,
		}).
		Find(&invalid).Error
	for _, job := range invalid {
		var count int64
		_ = w.deps.DB.Model(&models.Link{}).Where("id = ? AND uuid = ?", job.LinkID, job.LinkUUID).Count(&count).Error
		if count > 0 {
			continue
		}
		w.cancelDownloadPreparationByID(job.ID)
		w.cancelDownloadJob(&job, "source_unavailable", "This video is no longer available.")
	}

	cutoff := now.Add(-24 * time.Hour)
	purged := w.deps.DB.Unscoped().
		Where("status IN ? AND updated_at < ?", []string{
			models.DownloadJobStatusFailed,
			models.DownloadJobStatusCanceled,
			models.DownloadJobStatusExpired,
		}, cutoff).
		Delete(&models.DownloadJob{})

	partialFiles, partialBytes := w.removeTerminalDownloadPartials()
	orphanFiles, orphanBytes := w.removeOrphanedDownloadJobFiles(now)
	removedFiles += partialFiles + orphanFiles
	removedBytes += partialBytes + orphanBytes
	if removedFiles > 0 || purged.RowsAffected > 0 || len(expired) > 0 {
		log.Printf(
			"download_preparation event=cleanup expired=%d files=%d bytes=%d purged=%d",
			len(expired),
			removedFiles,
			removedBytes,
			purged.RowsAffected,
		)
	}
}

func (w *WorkerGroup) removeTerminalDownloadPartials() (int, int64) {
	var jobs []models.DownloadJob
	_ = w.deps.DB.
		Where("status IN ?", []string{
			models.DownloadJobStatusFailed,
			models.DownloadJobStatusCanceled,
			models.DownloadJobStatusExpired,
		}).
		Find(&jobs).Error
	var files int
	var bytes int64
	for _, job := range jobs {
		path := filepath.Join(w.downloadJobDirectory(), job.UUID+"."+job.Container+".part")
		size, removed := w.removeDownloadJobPath(path)
		if removed {
			files++
			bytes += size
		}
	}
	return files, bytes
}

func (w *WorkerGroup) removeOrphanedDownloadJobFiles(now time.Time) (int, int64) {
	dir := w.downloadJobDirectory()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	var jobs []models.DownloadJob
	_ = w.deps.DB.
		Where("status IN ?", []string{models.DownloadJobStatusPreparing, models.DownloadJobStatusReady}).
		Find(&jobs).Error
	referenced := make(map[string]bool, len(jobs)*2)
	for _, job := range jobs {
		if job.OutputPath != "" {
			referenced[filepath.Clean(job.OutputPath)] = true
		}
		referenced[filepath.Join(dir, job.UUID+"."+job.Container+".part")] = true
	}

	var files int
	var bytes int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if referenced[filepath.Clean(path)] {
			continue
		}
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) < time.Hour {
			continue
		}
		size, removed := w.removeDownloadJobPath(path)
		if removed {
			files++
			bytes += size
		}
	}
	return files, bytes
}

func (w *WorkerGroup) removeDownloadJobPath(path string) (int64, bool) {
	if !w.downloadJobPathInside(path) {
		return 0, false
	}
	pathAbs, _ := filepath.Abs(path)
	var size int64
	if info, err := os.Stat(pathAbs); err == nil {
		size = info.Size()
	}
	if err := os.Remove(pathAbs); err != nil {
		return 0, false
	}
	return size, true
}

func (w *WorkerGroup) cancelDownloadPreparationByID(id uint) {
	w.activePreparationsMu.Lock()
	active, exists := w.activePreparations[id]
	w.activePreparationsMu.Unlock()
	if exists && active.cancel != nil {
		active.cancel()
	}
}

func (w *WorkerGroup) CancelAllDownloadPreparations(reason string) {
	var jobs []models.DownloadJob
	_ = w.deps.DB.
		Where("status IN ?", []string{
			models.DownloadJobStatusQueued,
			models.DownloadJobStatusPreparing,
			models.DownloadJobStatusReady,
		}).
		Find(&jobs).Error

	for _, job := range jobs {
		w.cancelDownloadPreparationByID(job.ID)
		w.cancelDownloadJob(&job, "downloads_disabled", reason)
	}
}

func (w *WorkerGroup) CancelDownloadPreparationsForFile(fileID uint) {
	var jobs []models.DownloadJob
	_ = w.deps.DB.
		Where("file_id = ? AND status IN ?", fileID, []string{
			models.DownloadJobStatusQueued,
			models.DownloadJobStatusPreparing,
			models.DownloadJobStatusReady,
		}).
		Find(&jobs).Error
	for _, job := range jobs {
		w.cancelDownloadPreparationByID(job.ID)
		w.cancelDownloadJob(&job, "source_unavailable", "This video is no longer available.")
	}
}

func (w *WorkerGroup) HasActiveDownloadPreparationForFile(fileID uint) bool {
	w.activePreparationsMu.Lock()
	defer w.activePreparationsMu.Unlock()
	for _, active := range w.activePreparations {
		if active.fileID == fileID {
			return true
		}
	}
	return false
}
