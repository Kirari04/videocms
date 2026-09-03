package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ch/kirari04/videocms/background"
	"ch/kirari04/videocms/mediacache"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/services/tusupload"
	"ch/kirari04/videocms/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	taskMediaImport       = "media.import"
	taskMediaThumbnail    = "media.thumbnail"
	taskEncodeQuality     = "media.encode.quality"
	taskEncodeAudio       = "media.encode.audio"
	taskEncodeSubtitle    = "media.encode.subtitle"
	taskRemoteFetch       = "remote.fetch"
	taskDownloadPrepare   = "download.prepare"
	taskContentDelete     = "content.delete"
	taskAuditRecord       = "audit.record"
	taskSourceCleanup     = "maintenance.source_cleanup"
	taskDeletionReconcile = "maintenance.deletion_reconcile"
	taskDownloadCleanup   = "maintenance.download_cleanup"
	taskUploadCleanup     = "maintenance.upload_cleanup"
	taskAuditCleanup      = "maintenance.audit_cleanup"
	taskResourceCleanup   = "maintenance.resource_cleanup"
	taskJobRetention      = "maintenance.job_retention"
	taskTrafficRetention  = "maintenance.traffic_retention"
	taskStorageMigration  = "storage.migration.run"
	taskStorageCleanup    = "storage.migration.cleanup"
	taskStorageAbort      = "storage.migration.abort_cleanup"
	taskStorageReconcile  = "maintenance.storage_migrations"
	taskStorageCacheFill  = "storage.cache.fill"
	taskStorageCachePrune = "maintenance.storage_cache"
)

type encodingTaskPayload struct {
	Type   string `json:"type"`
	ID     uint   `json:"id"`
	FileID uint   `json:"fileId"`
}

type uploadImportPayload struct {
	UploadSessionID uint `json:"uploadSessionId"`
}

type remoteFetchPayload struct {
	RemoteDownloadID uint `json:"remoteDownloadId"`
}

type downloadPreparePayload struct {
	DownloadJobID uint `json:"downloadJobId"`
}

type thumbnailPayload struct {
	FileID uint `json:"fileId"`
}

type deletePayload struct {
	LinkIDs   []uint `json:"linkIds,omitempty"`
	FolderIDs []uint `json:"folderIds,omitempty"`
	UserID    uint   `json:"userId"`
	Admin     bool   `json:"admin"`
}

type auditPayload struct {
	APIKeyID uint   `json:"apiKeyId"`
	UserID   uint   `json:"userId"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	IP       string `json:"ip"`
}

func (w *WorkerGroup) RegisterBackgroundHandlers(runtime *background.Runtime, tus *tusupload.Service) error {
	if runtime == nil {
		return errors.New("background runtime is nil")
	}
	registrations := map[string]background.Handler{
		taskMediaImport:       w.mediaImportHandler(tus),
		taskMediaThumbnail:    w.thumbnailHandler,
		taskEncodeQuality:     w.encodingHandler,
		taskEncodeAudio:       w.encodingHandler,
		taskEncodeSubtitle:    w.encodingHandler,
		taskRemoteFetch:       w.remoteFetchHandler,
		taskDownloadPrepare:   w.downloadPreparationHandler,
		taskContentDelete:     w.contentDeleteHandler,
		taskAuditRecord:       w.auditRecordHandler,
		taskSourceCleanup:     w.sourceCleanupHandler(runtime),
		taskDeletionReconcile: w.deletionReconcileHandler,
		taskDownloadCleanup:   w.downloadCleanupHandler,
		taskUploadCleanup:     w.uploadCleanupHandler(tus),
		taskAuditCleanup:      w.auditCleanupHandler,
		taskResourceCleanup:   w.resourceCleanupHandler,
		taskJobRetention:      w.jobRetentionHandler(runtime),
		taskTrafficRetention:  w.trafficRetentionHandler,
		taskStorageMigration:  w.storageMigrationHandler(runtime),
		taskStorageCleanup:    w.storageMigrationCleanupHandler,
		taskStorageAbort:      w.storageMigrationAbortHandler(runtime),
		taskStorageReconcile:  w.storageMigrationReconcileHandler(runtime),
		taskStorageCacheFill:  w.storageCacheFillHandler,
		taskStorageCachePrune: w.storageCachePruneHandler,
	}
	for _, kind := range sortedHandlerKinds(registrations) {
		if err := runtime.Register(kind, registrations[kind]); err != nil {
			return err
		}
	}

	schedules := []background.ScheduleDefinition{
		maintenanceSchedule("source-cleanup", taskSourceCleanup, 5*time.Minute, true),
		maintenanceSchedule("deletion-reconciliation", taskDeletionReconcile, 5*time.Minute, true),
		maintenanceSchedule("prepared-download-expiry", taskDownloadCleanup, time.Minute, true),
		maintenanceSchedule("upload-expiry", taskUploadCleanup, time.Hour, true),
		maintenanceSchedule("api-audit-retention", taskAuditCleanup, time.Hour, true),
		maintenanceSchedule("resource-retention", taskResourceCleanup, time.Hour, false),
		maintenanceSchedule("background-history-retention", taskJobRetention, 24*time.Hour, false),
		maintenanceSchedule("traffic-history-retention", taskTrafficRetention, 24*time.Hour, false),
		maintenanceSchedule("storage-migration-reconciliation", taskStorageReconcile, time.Minute, true),
		maintenanceSchedule("storage-cache-eviction", taskStorageCachePrune, time.Minute, true),
	}
	for _, schedule := range schedules {
		if err := runtime.RegisterSchedule(schedule); err != nil {
			return err
		}
	}
	return nil
}

func maintenanceSchedule(key, kind string, interval time.Duration, runOnStart bool) background.ScheduleDefinition {
	return background.ScheduleDefinition{
		Key: key, Kind: kind, Queue: background.QueueMaintenance, Interval: interval, RunOnStart: runOnStart,
		Build: func() background.JobSpec {
			return background.JobSpec{
				Kind: kind, Visibility: background.VisibilitySystem, Label: maintenanceLabel(key),
				Tasks: []background.TaskSpec{{Kind: kind, Queue: background.QueueMaintenance, Phase: maintenanceLabel(key), DedupeKey: kind, Priority: 10, Required: true, Weight: 1}},
			}
		},
	}
}

func maintenanceLabel(key string) string {
	return strings.ReplaceAll(strings.TrimSpace(key), "-", " ")
}

func sortedHandlerKinds(handlers map[string]background.Handler) []string {
	order := []string{
		taskMediaImport, taskMediaThumbnail, taskEncodeQuality, taskEncodeAudio, taskEncodeSubtitle,
		taskRemoteFetch, taskDownloadPrepare, taskContentDelete, taskAuditRecord, taskSourceCleanup,
		taskDeletionReconcile, taskDownloadCleanup, taskUploadCleanup, taskAuditCleanup, taskResourceCleanup, taskJobRetention, taskTrafficRetention,
		taskStorageMigration, taskStorageCleanup, taskStorageAbort,
		taskStorageReconcile, taskStorageCacheFill, taskStorageCachePrune,
	}
	return order
}

func (w *WorkerGroup) storageCacheFillHandler(ctx context.Context, task background.Task) (background.Result, error) {
	if w.deps.MediaCache == nil {
		return background.Result{Phase: "Cache disabled"}, nil
	}
	var payload mediacache.PromotionPayload
	if err := decodeTaskPayload(task, &payload); err != nil {
		return background.Result{}, err
	}
	if err := w.deps.MediaCache.Fill(ctx, payload); err != nil {
		if mediacache.AdmissionSkipped(err) {
			_ = os.Remove(payload.TemporaryPath)
			return background.Result{Phase: "Cache admission skipped"}, nil
		}
		if ctx.Err() != nil {
			return background.Result{}, ctx.Err()
		}
		if os.IsNotExist(err) || errors.Is(err, storage.ErrNotFound) {
			return background.Result{}, background.Permanent("cache_source_missing", "The requested playback data is no longer available from primary storage", err)
		}
		return background.Result{}, background.Transient("cache_fill_failed", "Playback data could not be added to the cache", err)
	}
	_ = os.Remove(payload.TemporaryPath)
	return background.Result{Phase: "Playback data cached"}, nil
}

func (w *WorkerGroup) storageCachePruneHandler(ctx context.Context, _ background.Task) (background.Result, error) {
	if w.deps.MediaCache == nil {
		return background.Result{Phase: "Cache disabled"}, nil
	}
	if err := w.deps.MediaCache.Prune(ctx); err != nil {
		return background.Result{}, background.Transient("cache_eviction_failed", "Old playback cache data could not be removed", err)
	}
	return background.Result{Phase: "Playback cache within limits"}, nil
}

func decodeTaskPayload(task background.Task, target any) error {
	if task.PayloadVersion != 1 {
		return background.Permanent("payload_version_unsupported", "This task was created with an unsupported payload version", fmt.Errorf("task %s payload version %d", task.ID, task.PayloadVersion))
	}
	if err := json.Unmarshal([]byte(task.Payload), target); err != nil {
		return background.Permanent("payload_invalid", "The saved task payload is invalid", err)
	}
	return nil
}

func (w *WorkerGroup) encodingHandler(ctx context.Context, task background.Task) (background.Result, error) {
	var payload encodingTaskPayload
	if err := decodeTaskPayload(task, &payload); err != nil {
		return background.Result{}, err
	}
	if !encodingEnabled(w.Config()) {
		return background.Result{}, background.Canceled(errors.New("encoding disabled by administrator"))
	}
	var file models.File
	if err := w.deps.DB.First(&file, payload.FileID).Error; err != nil {
		return background.Result{}, background.Permanent("source_unavailable", "The source video is no longer available", err)
	}
	legacyTask := EncodingTask{Type: payload.Type, FileID: file.ID, FileUUID: file.UUID, StorageID: file.StorageID, ID: payload.ID}
	if w.deps.MediaCache != nil {
		if err := w.deps.MediaCache.InvalidateFile(context.WithoutCancel(ctx), file.ID); err != nil {
			return background.Result{}, background.Transient("cache_invalidation_failed", "Existing playback cache data could not be invalidated", err)
		}
	}
	if err := w.markEncodingStarted(payload.Type, payload.ID, task.ID); err != nil {
		return background.Result{}, background.Transient("state_update_failed", "The encoding state could not be updated", err)
	}
	encodeCtx, cancel := context.WithCancel(ctx)
	w.addActiveEncoding(ActiveEncoding{Task: legacyTask, Cancel: cancel})
	defer func() {
		cancel()
		w.deleteActiveEncoding(legacyTask)
	}()
	background.ReportProgress(encodeCtx, 0, "Starting encoder")
	err := w.runEncode(encodeCtx, legacyTask)
	var cacheErr error
	if w.deps.MediaCache != nil {
		cacheErr = w.deps.MediaCache.InvalidateFile(context.WithoutCancel(ctx), file.ID)
	}
	if encodeCtx.Err() != nil {
		_ = w.resetEncodingProjection(payload.Type, payload.ID)
		return background.Result{}, encodeCtx.Err()
	}
	if cacheErr != nil {
		return background.Result{}, background.Transient("cache_invalidation_failed", "Playback cache data could not be refreshed after encoding", cacheErr)
	}
	if err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "publish output") || strings.Contains(message, "prepare source") || strings.Contains(message, "database is") {
			return background.Result{}, background.Transient("encoding_io_failed", "Encoding encountered a temporary storage error", err)
		}
		return background.Result{}, background.Permanent("encoding_failed", "The media could not be encoded", err)
	}
	return background.Result{Phase: "Encoding complete"}, nil
}

func (w *WorkerGroup) markEncodingStarted(kind string, id uint, taskID string) error {
	updates := map[string]any{"background_task_id": taskID, "encoding": true, "failed": false, "error": "", "progress": 0}
	switch kind {
	case "quality":
		return w.deps.DB.Model(&models.Quality{}).Where("id = ?", id).Updates(updates).Error
	case "audio":
		return w.deps.DB.Model(&models.Audio{}).Where("id = ?", id).Updates(updates).Error
	case "subtitle":
		return w.deps.DB.Model(&models.Subtitle{}).Where("id = ?", id).Updates(updates).Error
	default:
		return fmt.Errorf("unknown encoding type %q", kind)
	}
}

func (w *WorkerGroup) resetEncodingProjection(kind string, id uint) error {
	updates := map[string]any{"encoding": false, "failed": false, "error": "", "progress": 0}
	switch kind {
	case "quality":
		return w.deps.DB.Model(&models.Quality{}).Where("id = ?", id).Updates(updates).Error
	case "audio":
		return w.deps.DB.Model(&models.Audio{}).Where("id = ?", id).Updates(updates).Error
	case "subtitle":
		return w.deps.DB.Model(&models.Subtitle{}).Where("id = ?", id).Updates(updates).Error
	default:
		return nil
	}
}

func (w *WorkerGroup) thumbnailHandler(ctx context.Context, task background.Task) (background.Result, error) {
	var payload thumbnailPayload
	if err := decodeTaskPayload(task, &payload); err != nil {
		return background.Result{}, err
	}
	var file models.File
	if err := w.deps.DB.First(&file, payload.FileID).Error; err != nil {
		return background.Result{}, background.Permanent("source_unavailable", "The source video is no longer available", err)
	}
	releaseFile := w.deps.StorageLifecycle.FileReadLock(file.ID)
	defer releaseFile()
	if err := w.deps.DB.WithContext(ctx).First(&file, payload.FileID).Error; err != nil {
		return background.Result{}, background.Permanent("source_unavailable", "The source video is no longer available", err)
	}
	releaseMount := w.deps.StorageLifecycle.ReadLock(file.StorageID)
	defer releaseMount()
	if file.AvgFrameRate <= 0 || file.SourceKey == "" {
		return background.Result{Phase: "Thumbnail not required"}, nil
	}
	key, err := storage.ParseKey(file.SourceKey)
	if err != nil {
		return background.Result{}, background.Permanent("source_invalid", "The stored media source is invalid", err)
	}
	background.ReportProgress(ctx, 0.1, "Loading thumbnail source")
	materialized, cleanup, err := w.deps.Storage.Materialize(ctx, file.StorageID, key, "thumbnail-source", filepath.Ext(key.String()))
	if err != nil {
		return background.Result{}, background.Transient("storage_unavailable", "The thumbnail source is temporarily unavailable", err)
	}
	defer cleanup()
	if !background.BeginCommit(ctx, "Publishing thumbnail") {
		return background.Result{}, context.Canceled
	}
	if _, err := w.logic.CreateThumbnailInStoreContext(ctx, 4, materialized, 1080, file.Thumbnail, file.UUID, file.StorageID, file.Duration, file.AvgFrameRate); err != nil {
		if ctx.Err() != nil {
			return background.Result{}, ctx.Err()
		}
		return background.Result{}, background.Permanent("thumbnail_failed", "The video thumbnail could not be generated", err)
	}
	background.ReportProgress(ctx, 0.99, "Publishing thumbnail")
	return background.Result{Phase: "Thumbnail complete"}, nil
}

func (w *WorkerGroup) mediaImportHandler(tus *tusupload.Service) background.Handler {
	return func(ctx context.Context, task background.Task) (background.Result, error) {
		var payload uploadImportPayload
		if err := decodeTaskPayload(task, &payload); err != nil {
			return background.Result{}, err
		}
		var session models.UploadSession
		if err := w.deps.DB.Unscoped().First(&session, payload.UploadSessionID).Error; err != nil {
			return background.Result{}, background.Permanent("upload_unavailable", "The uploaded file is no longer available", err)
		}
		var link *models.Link
		if session.LinkID > 0 {
			var existing models.Link
			if err := w.deps.DB.First(&existing, session.LinkID).Error; err != nil {
				return background.Result{}, background.Permanent("import_result_missing", "The imported video could not be found", err)
			}
			link = &existing
		} else if session.Protocol == models.UploadProtocolTus {
			if tus == nil {
				return background.Result{}, background.Transient("upload_service_unavailable", "The upload service is unavailable", errors.New("nil TUS service"))
			}
			background.ReportProgress(ctx, 0.05, "Validating upload")
			status, created, err := tus.ResumeFinalizeContext(ctx, session.TusID, session.UserID)
			if err != nil {
				if ctx.Err() != nil {
					return background.Result{}, ctx.Err()
				}
				if status >= 500 {
					return background.Result{}, background.Transient("import_failed", "The uploaded video could not be imported", err)
				}
				return background.Result{}, background.Permanent("invalid_upload", err.Error(), err)
			}
			link = created
		} else {
			created, err := w.finalizeSimpleSession(ctx, &session)
			if err != nil {
				return background.Result{}, err
			}
			link = created
		}
		if link == nil {
			return background.Result{}, background.Transient("import_result_missing", "The import did not produce a video", errors.New("missing link"))
		}
		children, err := w.mediaProcessingTasks(link.FileID, 80)
		if err != nil {
			return background.Result{}, background.Transient("processing_queue_failed", "The video was imported but processing could not be queued", err)
		}
		if err := w.deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&models.File{}).Where("id = ?", link.FileID).Update("background_job_id", task.JobID).Error; err != nil {
			return background.Result{}, background.Transient("state_update_failed", "The imported video could not be linked to its background job", err)
		}
		return background.Result{ResultType: "link", ResultID: link.UUID, Phase: "Import complete", Children: children}, nil
	}
}

func (w *WorkerGroup) finalizeSimpleSession(ctx context.Context, session *models.UploadSession) (*models.Link, error) {
	if session.LinkID > 0 {
		var existing models.Link
		if err := w.deps.DB.First(&existing, session.LinkID).Error; err != nil {
			return nil, background.Transient("import_result_missing", "The imported video could not be found", err)
		}
		return &existing, nil
	}
	if session.Status != models.UploadStatusUploaded && session.Status != models.UploadStatusFailed && session.Status != models.UploadStatusImporting {
		return nil, background.Permanent("upload_not_ready", "The upload is not ready to import", fmt.Errorf("status %s", session.Status))
	}
	if session.Status != models.UploadStatusImporting {
		claimed := w.deps.DB.Model(session).Where("id = ? AND status IN ?", session.ID, []string{models.UploadStatusUploaded, models.UploadStatusFailed}).Update("status", models.UploadStatusImporting)
		if claimed.Error != nil {
			return nil, background.Transient("state_update_failed", "The upload state could not be updated", claimed.Error)
		}
		if claimed.RowsAffected != 1 {
			return nil, background.Transient("import_already_running", "The upload is already being imported", errors.New("claim lost"))
		}
	}
	background.ReportProgress(ctx, 0.1, "Inspecting uploaded video")
	if !background.BeginCommit(ctx, "Importing uploaded video") {
		return nil, background.Canceled(context.Canceled)
	}
	originalPath := session.StoragePath
	status, link, cloned, err := w.logic.CreateFileContext(ctx, &session.StoragePath, session.ParentFolderID, session.Name, session.UUID, session.Size, session.UserID, session.ClientUploadUUID)
	if session.StoragePath != originalPath {
		if updateErr := w.deps.DB.WithContext(context.WithoutCancel(ctx)).Model(session).Update("storage_path", session.StoragePath).Error; updateErr != nil {
			return nil, background.Transient("state_update_failed", "The upload storage state could not be updated", updateErr)
		}
	}
	if err != nil {
		if updateErr := w.deps.DB.WithContext(context.WithoutCancel(ctx)).Model(session).Updates(map[string]any{"status": models.UploadStatusFailed, "error": boundedServiceError(err.Error())}).Error; updateErr != nil {
			return nil, background.Transient("state_update_failed", "The failed upload state could not be recorded", updateErr)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if status >= 500 {
			return nil, background.Transient("import_failed", "The uploaded video could not be imported", err)
		}
		return nil, background.Permanent("invalid_media", err.Error(), err)
	}
	if cloned && session.StoragePath != "" {
		_ = os.Remove(session.StoragePath)
	}
	now := time.Now()
	err = w.deps.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.UploadLog{}).Where("upload_session_id = ?", session.ID).Update("file_id", link.FileID).Error; err != nil {
			return err
		}
		return tx.Model(session).Updates(map[string]any{"status": models.UploadStatusDone, "file_id": link.FileID, "link_id": link.ID, "finalized_at": &now, "error": ""}).Error
	})
	if err != nil {
		return nil, background.Transient("state_update_failed", "The imported video could not be finalized", err)
	}
	return link, nil
}

func (w *WorkerGroup) mediaProcessingTasks(fileID uint, totalWeight int) ([]background.TaskSpec, error) {
	var file models.File
	if err := w.deps.DB.Preload("Qualitys").Preload("Audios").Preload("Subtitles").First(&file, fileID).Error; err != nil {
		return nil, err
	}
	count := len(file.Qualitys) + len(file.Audios) + len(file.Subtitles)
	if file.AvgFrameRate > 0 && file.SourceKey != "" {
		count++
	}
	if count == 0 {
		return nil, nil
	}
	weight := totalWeight / count
	if weight < 1 {
		weight = 1
	}
	tasks := make([]background.TaskSpec, 0, count)
	if file.AvgFrameRate > 0 && file.SourceKey != "" {
		tasks = append(tasks, background.TaskSpec{Kind: taskMediaThumbnail, Queue: background.QueueFFmpeg, Phase: "Generating thumbnail", Payload: thumbnailPayload{FileID: file.ID}, DedupeKey: fmt.Sprintf("thumbnail:%d", file.ID), Priority: 30, Required: false, Weight: weight})
	}
	if encodingEnabled(w.Config()) {
		for _, item := range file.Subtitles {
			if !item.Ready {
				tasks = append(tasks, encodingSpec("subtitle", item.ID, file.ID, item.Name, 40, weight))
			}
		}
		for _, item := range file.Audios {
			if !item.Ready {
				tasks = append(tasks, encodingSpec("audio", item.ID, file.ID, item.Name, 35, weight))
			}
		}
		for _, item := range file.Qualitys {
			if !item.Ready {
				tasks = append(tasks, encodingSpec("quality", item.ID, file.ID, item.Name, 25, weight))
			}
		}
	}
	return tasks, nil
}

func encodingSpec(kind string, id, fileID uint, name string, priority, weight int) background.TaskSpec {
	return background.TaskSpec{Kind: "media.encode." + kind, Queue: background.QueueFFmpeg, Phase: "Encoding " + name, Payload: encodingTaskPayload{Type: kind, ID: id, FileID: fileID}, DedupeKey: fmt.Sprintf("%s:%d", kind, id), Priority: priority, Required: false, Weight: weight}
}

func (w *WorkerGroup) remoteFetchHandler(ctx context.Context, task background.Task) (background.Result, error) {
	var payload remoteFetchPayload
	if err := decodeTaskPayload(task, &payload); err != nil {
		return background.Result{}, err
	}
	if !w.remoteDownloadsEnabled() {
		return background.Result{}, background.Canceled(errors.New("remote downloads disabled"))
	}
	var remote models.RemoteDownload
	if err := w.deps.DB.First(&remote, payload.RemoteDownloadID).Error; err != nil {
		return background.Result{}, background.Permanent("download_missing", "The remote download no longer exists", err)
	}
	if err := validateRemoteURLScheme(remote.Url); err != nil {
		return background.Result{}, background.Permanent("invalid_url", "The remote URL is invalid", err)
	}
	if remote.Status == models.RemoteDownloadStatusCompleted && remote.LinkID > 0 {
		children, err := w.mediaProcessingTasks(remote.FileID, 40)
		if err != nil {
			return background.Result{}, background.Transient("processing_queue_failed", "The downloaded video processing could not be queued", err)
		}
		return background.Result{ResultType: "link", ResultID: remote.LinkUUID, Phase: "Download imported", Children: children}, nil
	}
	if remote.Status != models.RemoteDownloadStatusImporting {
		now := time.Now()
		if err := w.deps.DB.WithContext(ctx).Model(&remote).Updates(map[string]any{"status": models.RemoteDownloadStatusDownloading, "started_at": &now, "finished_at": nil, "error": "", "progress": 0, "background_job_id": task.JobID}).Error; err != nil {
			return background.Result{}, background.Transient("state_update_failed", "The download state could not be updated", err)
		}
		remote.Status, remote.StartedAt = models.RemoteDownloadStatusDownloading, &now
	} else {
		if err := w.deps.DB.WithContext(ctx).Model(&remote).Update("background_job_id", task.JobID).Error; err != nil {
			return background.Result{}, background.Transient("state_update_failed", "The download could not be linked to its background job", err)
		}
		background.ReportProgress(ctx, 0.95, "Resuming remote import")
	}
	w.processDownload(ctx, remote)
	if ctx.Err() != nil {
		finish := time.Now()
		if err := w.deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&remote).Updates(map[string]any{"status": models.RemoteDownloadStatusCanceled, "error": "Download canceled", "finished_at": &finish, "canceled_at": &finish, "temp_path": ""}).Error; err != nil {
			return background.Result{}, background.Transient("state_update_failed", "The canceled download state could not be recorded", err)
		}
		return background.Result{}, ctx.Err()
	}
	if err := w.deps.DB.First(&remote, remote.ID).Error; err != nil {
		return background.Result{}, background.Transient("state_load_failed", "The download result could not be loaded", err)
	}
	switch remote.Status {
	case models.RemoteDownloadStatusCompleted:
		children, err := w.mediaProcessingTasks(remote.FileID, 40)
		if err != nil {
			return background.Result{}, background.Transient("processing_queue_failed", "The download completed but processing could not be queued", err)
		}
		return background.Result{ResultType: "link", ResultID: remote.LinkUUID, Phase: "Download imported", Children: children}, nil
	case models.RemoteDownloadStatusCanceled:
		return background.Result{}, background.Canceled(errors.New(remote.Error))
	default:
		cause := errors.New(remote.Error)
		lower := strings.ToLower(remote.Error)
		if strings.Contains(lower, "network error") || strings.Contains(lower, "timeout") || strings.Contains(lower, "http error: 5") || strings.Contains(lower, "http error: 429") {
			return background.Result{}, background.Transient("remote_unavailable", "The remote server is temporarily unavailable", cause)
		}
		return background.Result{}, background.Permanent("remote_download_failed", "The remote video could not be downloaded", cause)
	}
}

func (w *WorkerGroup) downloadPreparationHandler(ctx context.Context, task background.Task) (background.Result, error) {
	var payload downloadPreparePayload
	if err := decodeTaskPayload(task, &payload); err != nil {
		return background.Result{}, err
	}
	if !w.downloadPreparationsEnabled() {
		return background.Result{}, background.Canceled(errors.New("downloads disabled"))
	}
	var job models.DownloadJob
	if err := w.deps.DB.First(&job, payload.DownloadJobID).Error; err != nil {
		return background.Result{}, background.Permanent("preparation_missing", "The prepared download no longer exists", err)
	}
	if job.Status == models.DownloadJobStatusReady && (job.ExpiresAt == nil || job.ExpiresAt.After(time.Now())) && w.downloadJobPathInside(job.OutputPath) {
		if _, err := os.Stat(job.OutputPath); err == nil {
			return background.Result{ResultType: "download_preparation", ResultID: job.UUID, Phase: "Download ready"}, nil
		}
	}
	now := time.Now()
	if err := w.deps.DB.Model(&job).Updates(map[string]any{"status": models.DownloadJobStatusPreparing, "started_at": &now, "finished_at": nil, "error_code": "", "error_message": "", "progress": 0, "background_job_id": task.JobID}).Error; err != nil {
		return background.Result{}, background.Transient("state_update_failed", "The download preparation could not start", err)
	}
	job.Status, job.StartedAt = models.DownloadJobStatusPreparing, &now
	preparationCtx, cancel := context.WithCancel(ctx)
	w.registerDownloadPreparation(job, cancel)
	defer func() {
		cancel()
		w.unregisterDownloadPreparation(job.ID)
	}()
	w.processDownloadPreparation(preparationCtx, job)
	if preparationCtx.Err() != nil {
		w.cancelDownloadJob(&job, "canceled", "Download preparation canceled")
		return background.Result{}, preparationCtx.Err()
	}
	if err := w.deps.DB.First(&job, job.ID).Error; err != nil {
		return background.Result{}, background.Transient("state_load_failed", "The prepared download result could not be loaded", err)
	}
	switch job.Status {
	case models.DownloadJobStatusReady:
		return background.Result{ResultType: "download_preparation", ResultID: job.UUID, Phase: "Download ready"}, nil
	case models.DownloadJobStatusCanceled:
		return background.Result{}, background.Canceled(errors.New(job.ErrorMessage))
	default:
		cause := errors.New(job.ErrorMessage)
		if job.ErrorCode == "storage_unavailable" || job.ErrorCode == "preparation_timeout" {
			return background.Result{}, background.Transient(job.ErrorCode, job.ErrorMessage, cause)
		}
		return background.Result{}, background.Permanent(nonempty(job.ErrorCode, "preparation_failed"), nonempty(job.ErrorMessage, "The download could not be prepared"), cause)
	}
}

func (w *WorkerGroup) contentDeleteHandler(ctx context.Context, task background.Task) (background.Result, error) {
	var payload deletePayload
	if err := decodeTaskPayload(task, &payload); err != nil {
		return background.Result{}, err
	}
	background.ReportProgress(ctx, 0.05, "Validating content")
	linkIDs, err := w.remainingDeletionIDs(ctx, "links", payload.LinkIDs, payload.UserID, payload.Admin)
	if err != nil {
		return background.Result{}, background.Transient("deletion_reconciliation_failed", "The deletion state could not be checked", err)
	}
	folderIDs, err := w.remainingDeletionIDs(ctx, "folders", payload.FolderIDs, payload.UserID, payload.Admin)
	if err != nil {
		return background.Result{}, background.Transient("deletion_reconciliation_failed", "The deletion state could not be checked", err)
	}
	if !background.BeginCommit(ctx, "Deleting content") {
		if ctx.Err() != nil {
			return background.Result{}, ctx.Err()
		}
		return background.Result{}, background.Canceled(errors.New("deletion canceled before side effects"))
	}
	if len(linkIDs) > 0 {
		items := make([]models.LinkDeleteValidation, 0, len(linkIDs))
		for _, id := range linkIDs {
			items = append(items, models.LinkDeleteValidation{LinkID: id})
		}
		status, err := w.logic.DeleteFiles(&models.LinksDeleteValidation{LinkIDs: items}, payload.UserID, payload.Admin)
		if err != nil {
			return background.Result{}, classifiedHTTPError(status, "delete_failed", "The selected videos could not be deleted", err)
		}
	}
	if len(folderIDs) > 0 {
		items := make([]models.FolderDeleteValidation, 0, len(folderIDs))
		for _, id := range folderIDs {
			items = append(items, models.FolderDeleteValidation{FolderID: id})
		}
		status, err := w.logic.DeleteFolders(&models.FoldersDeleteValidation{FolderIDs: items}, payload.UserID, payload.Admin)
		if err != nil {
			return background.Result{}, classifiedHTTPError(status, "delete_failed", "The selected folders could not be deleted", err)
		}
	}
	// Logical deletion is the user-visible operation. Physical storage cleanup
	// is reconciled independently by the maintenance schedule, so an unrelated
	// broken storage object cannot fail this user's deletion job.
	background.ReportProgress(ctx, 0.99, "Deletion recorded; storage cleanup scheduled")
	return background.Result{Phase: "Deletion complete"}, nil
}

func (w *WorkerGroup) remainingDeletionIDs(ctx context.Context, table string, ids []uint, userID uint, admin bool) ([]uint, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	query := w.deps.DB.WithContext(ctx).Table(table).Where("id IN ? AND deleted_at IS NULL", ids)
	if !admin {
		query = query.Where("user_id = ?", userID)
	}
	var remaining []uint
	if err := query.Pluck("id", &remaining).Error; err != nil {
		return nil, err
	}
	return remaining, nil
}

func (w *WorkerGroup) auditRecordHandler(ctx context.Context, task background.Task) (background.Result, error) {
	var payload auditPayload
	if err := decodeTaskPayload(task, &payload); err != nil {
		return background.Result{}, err
	}
	now := time.Now()
	err := w.deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.ApiKey{}).Where("id = ?", payload.APIKeyID).Update("last_used_at", &now).Error; err != nil {
			return err
		}
		record := models.ApiKeyAuditLog{BackgroundTaskID: task.ID, ApiKeyID: payload.APIKeyID, UserID: payload.UserID, Method: payload.Method, Path: payload.Path, IP: payload.IP}
		return tx.Where("background_task_id = ?", task.ID).FirstOrCreate(&record).Error
	})
	if err != nil {
		return background.Result{}, background.Transient("audit_write_failed", "The API-key audit record could not be saved", err)
	}
	return background.Result{}, nil
}

func (w *WorkerGroup) sourceCleanupHandler(runtime *background.Runtime) background.Handler {
	return func(ctx context.Context, _ background.Task) (background.Result, error) {
		if err := w.reconcileEncodingTasks(ctx, runtime); err != nil {
			return background.Result{}, background.Transient("encoding_reconciliation_failed", "Interrupted encodes could not be reconciled", err)
		}
		if err := w.runEncoderCleanup(); err != nil {
			return background.Result{}, background.Transient("source_cleanup_failed", "Encoded source files could not be cleaned up", err)
		}
		return background.Result{}, nil
	}
}

type pendingEncodingArtifact struct {
	kind           string
	id             uint
	fileID         uint
	name           string
	priority       int
	previousTaskID string
}

// reconcileEncodingTasks closes the feature-toggle and crash-recovery gap.
// Canceled artifacts are projected back to a non-failed state by their
// handler; once encoding is enabled again this creates one fresh durable task.
func (w *WorkerGroup) reconcileEncodingTasks(ctx context.Context, runtime *background.Runtime) error {
	if runtime == nil || !encodingEnabled(w.Config()) {
		return nil
	}
	pendingByFile := make(map[uint][]pendingEncodingArtifact)
	appendPending := func(kind string, id, fileID uint, name string, priority int, taskID string) error {
		if taskID == "" {
			var existing background.Task
			err := runtime.DB().WithContext(ctx).
				Where("kind = ? AND dedupe_key = ? AND status NOT IN ?", "media.encode."+kind, fmt.Sprintf("%s:%d", kind, id), []string{background.TaskFailed, background.TaskCanceled}).
				Order("created_at DESC").First(&existing).Error
			if err == nil {
				return w.setEncodingTaskReference(kind, id, existing.ID)
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if taskID != "" {
			var existing background.Task
			err := runtime.DB().WithContext(ctx).Select("status").First(&existing, "id = ?", taskID).Error
			if err == nil && existing.Status != background.TaskFailed && existing.Status != background.TaskCanceled {
				return nil
			}
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		pendingByFile[fileID] = append(pendingByFile[fileID], pendingEncodingArtifact{
			kind: kind, id: id, fileID: fileID, name: name, priority: priority, previousTaskID: taskID,
		})
		return nil
	}

	var qualities []models.Quality
	if err := w.deps.DB.WithContext(ctx).Where("ready = ? AND failed = ?", false, false).Find(&qualities).Error; err != nil {
		return err
	}
	for _, item := range qualities {
		if err := appendPending("quality", item.ID, item.FileID, item.Name, 25, item.BackgroundTaskID); err != nil {
			return err
		}
	}
	var audios []models.Audio
	if err := w.deps.DB.WithContext(ctx).Where("ready = ? AND failed = ?", false, false).Find(&audios).Error; err != nil {
		return err
	}
	for _, item := range audios {
		if err := appendPending("audio", item.ID, item.FileID, item.Name, 35, item.BackgroundTaskID); err != nil {
			return err
		}
	}
	var subtitles []models.Subtitle
	if err := w.deps.DB.WithContext(ctx).Where("ready = ? AND failed = ?", false, false).Find(&subtitles).Error; err != nil {
		return err
	}
	for _, item := range subtitles {
		if err := appendPending("subtitle", item.ID, item.FileID, item.Name, 40, item.BackgroundTaskID); err != nil {
			return err
		}
	}

	fileIDs := make([]uint, 0, len(pendingByFile))
	for fileID := range pendingByFile {
		fileIDs = append(fileIDs, fileID)
	}
	sort.Slice(fileIDs, func(i, j int) bool { return fileIDs[i] < fileIDs[j] })
	for _, fileID := range fileIDs {
		var file models.File
		if err := w.deps.DB.WithContext(ctx).First(&file, fileID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return err
		}
		artifacts := pendingByFile[fileID]
		sort.Slice(artifacts, func(i, j int) bool {
			if artifacts[i].kind == artifacts[j].kind {
				return artifacts[i].id < artifacts[j].id
			}
			return artifacts[i].kind < artifacts[j].kind
		})
		tasks := make([]background.TaskSpec, 0, len(artifacts))
		generationParts := make([]string, 0, len(artifacts))
		for _, artifact := range artifacts {
			tasks = append(tasks, encodingSpec(artifact.kind, artifact.id, artifact.fileID, artifact.name, artifact.priority, 1))
			generationParts = append(generationParts, fmt.Sprintf("%s:%d:%s", artifact.kind, artifact.id, artifact.previousTaskID))
		}
		generation := uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join(generationParts, "|"))).String()
		ownerID := file.UserID
		job, _, err := runtime.Enqueue(ctx, background.JobSpec{
			Kind: "media.process", Visibility: background.VisibilityUser, OwnerID: &ownerID,
			SubjectType: "file", SubjectID: file.UUID, IdempotencyKey: "reconcile:media-process:" + generation,
			Label: "Resume processing " + file.UUID, Tasks: tasks,
		})
		if err != nil {
			return err
		}
		var created []background.Task
		if err := runtime.DB().WithContext(ctx).Where("job_id = ?", job.ID).Find(&created).Error; err != nil {
			return err
		}
		for _, task := range created {
			parts := strings.SplitN(task.DedupeKey, ":", 2)
			if len(parts) != 2 {
				continue
			}
			var id uint
			if _, err := fmt.Sscan(parts[1], &id); err != nil {
				continue
			}
			if err := w.setEncodingTaskReference(parts[0], id, task.ID); err != nil {
				return err
			}
		}
		if err := w.deps.DB.WithContext(ctx).Model(&models.File{}).Where("id = ?", file.ID).Update("background_job_id", job.ID).Error; err != nil {
			return err
		}
	}
	return nil
}

func (w *WorkerGroup) setEncodingTaskReference(kind string, id uint, taskID string) error {
	switch kind {
	case "quality":
		return w.deps.DB.Model(&models.Quality{}).Where("id = ?", id).Update("background_task_id", taskID).Error
	case "audio":
		return w.deps.DB.Model(&models.Audio{}).Where("id = ?", id).Update("background_task_id", taskID).Error
	case "subtitle":
		return w.deps.DB.Model(&models.Subtitle{}).Where("id = ?", id).Update("background_task_id", taskID).Error
	default:
		return fmt.Errorf("unknown encoding artifact kind %q", kind)
	}
}

func (w *WorkerGroup) deletionReconcileHandler(context.Context, background.Task) (background.Result, error) {
	if err := w.runDeleter(); err != nil {
		return background.Result{}, background.Transient("deletion_reconciliation_failed", "Pending media deletions could not be reconciled", err)
	}
	return background.Result{}, nil
}

func (w *WorkerGroup) downloadCleanupHandler(context.Context, background.Task) (background.Result, error) {
	if err := w.runDownloadPreparationCleanup(); err != nil {
		return background.Result{}, background.Transient("download_cleanup_failed", "Expired prepared downloads could not be cleaned up", err)
	}
	return background.Result{}, nil
}

func (w *WorkerGroup) uploadCleanupHandler(tus *tusupload.Service) background.Handler {
	return func(context.Context, background.Task) (background.Result, error) {
		if tus == nil {
			return background.Result{}, background.Transient("upload_service_unavailable", "The upload service is unavailable", errors.New("nil TUS service"))
		}
		if err := tus.CleanupExpiredOnceE(); err != nil {
			return background.Result{}, background.Transient("upload_cleanup_failed", "Expired uploads could not be cleaned up", err)
		}
		return background.Result{}, nil
	}
}

func (w *WorkerGroup) auditCleanupHandler(context.Context, background.Task) (background.Result, error) {
	if err := w.runAuditCleanup(); err != nil {
		return background.Result{}, background.Transient("audit_cleanup_failed", "Expired API audit entries could not be removed", err)
	}
	return background.Result{}, nil
}

func (w *WorkerGroup) resourceCleanupHandler(ctx context.Context, _ background.Task) (background.Result, error) {
	err := w.deps.DB.WithContext(ctx).Where("created_at < ?", time.Now().AddDate(0, 0, -30)).Unscoped().Delete(&models.SystemResource{}).Error
	if err != nil {
		return background.Result{}, background.Transient("resource_cleanup_failed", "Old resource samples could not be removed", err)
	}
	return background.Result{}, nil
}

func (w *WorkerGroup) jobRetentionHandler(runtime *background.Runtime) background.Handler {
	return func(ctx context.Context, _ background.Task) (background.Result, error) {
		if _, err := runtime.Retain(ctx, time.Now().AddDate(0, 0, -30)); err != nil {
			return background.Result{}, background.Transient("history_cleanup_failed", "Old background-job history could not be removed", err)
		}
		return background.Result{}, nil
	}
}

func (w *WorkerGroup) trafficRetentionHandler(ctx context.Context, _ background.Task) (background.Result, error) {
	const batchSize = 5_000
	cutoff := time.Now().UTC().AddDate(0, 0, -90).Unix()
	deleted := int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return background.Result{}, err
		}
		var ids []uint
		if err := w.deps.DB.WithContext(ctx).Model(&models.TrafficLog{}).
			Where("bucket_start > 0 AND bucket_start < ?", cutoff).
			Order("id ASC").Limit(batchSize).Pluck("id", &ids).Error; err != nil {
			return background.Result{}, background.Transient("traffic_retention_failed", "Expired traffic statistics could not be selected", err)
		}
		if len(ids) == 0 {
			return background.Result{Phase: fmt.Sprintf("Traffic history retained; %d old rows removed", deleted)}, nil
		}
		result := w.deps.DB.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&models.TrafficLog{})
		if result.Error != nil {
			return background.Result{}, background.Transient("traffic_retention_failed", "Expired traffic statistics could not be removed", result.Error)
		}
		deleted += result.RowsAffected
	}
}

func classifiedHTTPError(status int, code, public string, err error) error {
	if status >= http.StatusInternalServerError {
		return background.Transient(code, public, err)
	}
	return background.Permanent(code, err.Error(), err)
}

func boundedServiceError(value string) string {
	if len(value) > 1000 {
		return value[:1000]
	}
	return value
}

func nonempty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
