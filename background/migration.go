package background

import (
	"errors"
	"fmt"
	"time"

	"ch/kirari04/videocms/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const legacyCutoverMigration = "unified-background-work-v1"

// BackfillLegacy performs the single-cutover migration. It is deliberately
// idempotent: a completed marker prevents repeat work and every generated job
// also has a stable legacy idempotency key.
func BackfillLegacy(db *gorm.DB, now time.Time) error {
	if db == nil {
		return errors.New("database unavailable")
	}
	var marker MigrationState
	if err := db.First(&marker, "key = ?", legacyCutoverMigration).Error; err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	cutoff := now.AddDate(0, 0, -30)
	steps := []struct {
		name string
		run  func(*gorm.DB, time.Time, time.Time) error
	}{
		{"encodings", backfillEncodingJobs},
		{"remote downloads", backfillRemoteJobs},
		{"prepared downloads", backfillPreparedJobs},
		{"uploads", backfillUploadJobs},
	}
	// Commit each independent legacy domain separately. A large installation
	// therefore does not hold one write transaction across the whole cutover,
	// while stable legacy keys make a partially completed run safe to resume.
	for _, step := range steps {
		if err := db.Transaction(func(tx *gorm.DB) error { return step.run(tx, cutoff, now) }); err != nil {
			return fmt.Errorf("backfill %s: %w", step.name, err)
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		return tx.Where("key = ?", legacyCutoverMigration).FirstOrCreate(&MigrationState{Key: legacyCutoverMigration, CompletedAt: now}).Error
	})
}

type encodingArtifact struct {
	kind      string
	id        uint
	fileID    uint
	name      string
	ready     bool
	failed    bool
	encoding  bool
	progress  float64
	errorText string
	createdAt *time.Time
	taskID    string
}

func backfillEncodingJobs(tx *gorm.DB, cutoff, now time.Time) error {
	fileIDs := make(map[uint]struct{})
	collect := func(table string) error {
		var ids []uint
		if err := tx.Table(table).
			Where("encoding = ? OR (ready = ? AND failed = ?) OR updated_at >= ?", true, false, false, cutoff).
			Distinct("file_id").
			Pluck("file_id", &ids).Error; err != nil {
			return err
		}
		for _, id := range ids {
			fileIDs[id] = struct{}{}
		}
		return nil
	}
	for _, table := range []string{"qualities", "audios", "subtitles"} {
		if err := collect(table); err != nil {
			return err
		}
	}
	for fileID := range fileIDs {
		var file models.File
		if err := tx.Unscoped().First(&file, fileID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return err
		}
		job, existed, err := legacyJob(tx, Job{
			ID: uuid.NewString(), Kind: "media.process", Status: JobQueued, Visibility: VisibilityUser,
			OwnerID: &file.UserID, SubjectType: "file", SubjectID: file.UUID,
			IdempotencyKey: fmt.Sprintf("legacy:media-process:%d", file.ID), Label: "Process " + file.UUID,
			CreatedAt: valueTime(file.CreatedAt, now),
		})
		if err != nil {
			return err
		}
		if existed {
			continue
		}
		artifacts, err := loadEncodingArtifacts(tx, fileID)
		if err != nil {
			return err
		}
		for _, artifact := range artifacts {
			status := TaskQueued
			attemptStatus := ""
			progress := clampProgress(artifact.progress)
			errorCode, errorMessage := "", ""
			if artifact.ready {
				status, attemptStatus, progress = TaskSucceeded, AttemptSucceeded, 10000
			} else if artifact.failed {
				status, attemptStatus = TaskFailed, AttemptFailed
				errorCode, errorMessage = "legacy_encoding_failed", boundedMessage(artifact.errorText, 512)
				if errorMessage == "" {
					errorMessage = "Encoding failed before the background-work upgrade"
				}
			} else if artifact.encoding {
				attemptStatus = AttemptInterrupted
			}
			task, err := createTask(tx, job.ID, TaskSpec{
				Kind: "media.encode." + artifact.kind, Queue: QueueFFmpeg, Phase: "Encoding " + artifact.name,
				Payload:   map[string]any{"type": artifact.kind, "id": artifact.id, "fileId": artifact.fileID},
				DedupeKey: fmt.Sprintf("%s:%d", artifact.kind, artifact.id), Priority: encodingPriority(artifact.kind), Required: false, Weight: 1,
			})
			if err != nil {
				return err
			}
			updates := map[string]any{"status": status, "progress": progress, "error_code": errorCode, "error_message": errorMessage}
			if terminalTaskStatus(status) {
				updates["finished_at"] = now
			}
			if attemptStatus != "" {
				updates["attempt_count"] = 1
				attempt := Attempt{ID: uuid.NewString(), TaskID: task.ID, Number: 1, Status: attemptStatus, Worker: "legacy", StartedAt: valueTime(artifact.createdAt, now), FinishedAt: &now, ErrorCode: errorCode, ErrorMessage: errorMessage}
				if err := tx.Create(&attempt).Error; err != nil {
					return err
				}
			}
			if err := tx.Model(task).Updates(updates).Error; err != nil {
				return err
			}
			if err := setEncodingTaskReference(tx, artifact.kind, artifact.id, task.ID); err != nil {
				return err
			}
		}
		if err := tx.Model(&models.File{}).Unscoped().Where("id = ?", file.ID).Update("background_job_id", job.ID).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Quality{}).Where("file_id = ?", file.ID).Updates(map[string]any{"encoding": false, "progress": 0}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Audio{}).Where("file_id = ?", file.ID).Updates(map[string]any{"encoding": false, "progress": 0}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.Subtitle{}).Where("file_id = ?", file.ID).Updates(map[string]any{"encoding": false, "progress": 0}).Error; err != nil {
			return err
		}
		if err := recomputeJob(tx, job, now); err != nil {
			return err
		}
	}
	return nil
}

func loadEncodingArtifacts(tx *gorm.DB, fileID uint) ([]encodingArtifact, error) {
	var qualities []models.Quality
	var audios []models.Audio
	var subtitles []models.Subtitle
	if err := tx.Where("file_id = ?", fileID).Find(&qualities).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("file_id = ?", fileID).Find(&audios).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("file_id = ?", fileID).Find(&subtitles).Error; err != nil {
		return nil, err
	}
	result := make([]encodingArtifact, 0, len(qualities)+len(audios)+len(subtitles))
	for _, item := range qualities {
		result = append(result, encodingArtifact{"quality", item.ID, item.FileID, item.Name, item.Ready, item.Failed, item.Encoding, item.Progress, item.Error, item.CreatedAt, item.BackgroundTaskID})
	}
	for _, item := range audios {
		result = append(result, encodingArtifact{"audio", item.ID, item.FileID, item.Name, item.Ready, item.Failed, item.Encoding, item.Progress, item.Error, item.CreatedAt, item.BackgroundTaskID})
	}
	for _, item := range subtitles {
		result = append(result, encodingArtifact{"subtitle", item.ID, item.FileID, item.Name, item.Ready, item.Failed, item.Encoding, item.Progress, item.Error, item.CreatedAt, item.BackgroundTaskID})
	}
	return result, nil
}

func backfillRemoteJobs(tx *gorm.DB, cutoff, now time.Time) error {
	var rows []models.RemoteDownload
	if err := tx.Unscoped().Where("status IN ? OR updated_at >= ?", models.ActiveRemoteDownloadStatuses(), cutoff).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		job, existed, err := legacyJob(tx, Job{ID: uuid.NewString(), Kind: "remote.ingest", Status: JobQueued, Visibility: VisibilityUser, OwnerID: &row.UserID, SubjectType: "remote_download", SubjectID: fmt.Sprint(row.ID), IdempotencyKey: fmt.Sprintf("legacy:remote-download:%d", row.ID), Label: safeRemoteLabel(row.Name), CreatedAt: valueTime(row.CreatedAt, now)})
		if err != nil {
			return err
		}
		if existed {
			continue
		}
		task, err := createTask(tx, job.ID, TaskSpec{Kind: "remote.fetch", Queue: QueueNetwork, Phase: "Downloading", Payload: map[string]any{"remoteDownloadId": row.ID}, DedupeKey: fmt.Sprintf("remote:%d", row.ID), Priority: 30, Required: true, Weight: 60})
		if err != nil {
			return err
		}
		status, attemptStatus, code, message, progress := TaskQueued, "", "", "", clampProgress(row.Progress)
		switch row.Status {
		case models.RemoteDownloadStatusCompleted:
			status, attemptStatus, progress = TaskSucceeded, AttemptSucceeded, 10000
		case models.RemoteDownloadStatusFailed:
			status, attemptStatus, code, message = TaskFailed, AttemptFailed, "legacy_remote_failed", boundedMessage(row.Error, 512)
		case models.RemoteDownloadStatusCanceled:
			status, attemptStatus, code, message = TaskCanceled, AttemptCanceled, "canceled", "Download canceled"
		case models.RemoteDownloadStatusDownloading, models.RemoteDownloadStatusImporting, models.RemoteDownloadStatusCanceling:
			attemptStatus = AttemptInterrupted
			if err := tx.Model(&row).Updates(map[string]any{"status": models.RemoteDownloadStatusPending, "progress": 0, "started_at": nil, "finished_at": nil}).Error; err != nil {
				return err
			}
		}
		if err := finishLegacyTask(tx, task, status, attemptStatus, code, message, progress, now); err != nil {
			return err
		}
		if err := tx.Model(&row).Update("background_job_id", job.ID).Error; err != nil {
			return err
		}
		if row.LinkUUID != "" {
			if err := tx.Model(job).Updates(map[string]any{"result_type": "link", "result_id": row.LinkUUID}).Error; err != nil {
				return err
			}
		}
		if err := recomputeJob(tx, job, now); err != nil {
			return err
		}
	}
	return nil
}

func backfillPreparedJobs(tx *gorm.DB, cutoff, now time.Time) error {
	active := []string{models.DownloadJobStatusQueued, models.DownloadJobStatusPreparing, models.DownloadJobStatusReady}
	var rows []models.DownloadJob
	if err := tx.Unscoped().Where("status IN ? OR updated_at >= ?", active, cutoff).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		owner := row.UserID
		job, existed, err := legacyJob(tx, Job{ID: uuid.NewString(), Kind: "download.prepare", Status: JobQueued, Visibility: VisibilityUser, OwnerID: &owner, SubjectType: "download_preparation", SubjectID: row.UUID, IdempotencyKey: fmt.Sprintf("legacy:download-preparation:%d", row.ID), Label: "Prepare " + row.OutputName, CreatedAt: valueTime(row.CreatedAt, now)})
		if err != nil {
			return err
		}
		if existed {
			continue
		}
		task, err := createTask(tx, job.ID, TaskSpec{Kind: "download.prepare", Queue: QueueFFmpeg, Phase: "Preparing download", Payload: map[string]any{"downloadJobId": row.ID}, DedupeKey: fmt.Sprintf("prepared:%d", row.ID), Priority: 50, Required: true, Weight: 100})
		if err != nil {
			return err
		}
		status, attemptStatus, code, message, progress := TaskQueued, "", "", "", clampProgress(row.Progress)
		switch row.Status {
		case models.DownloadJobStatusReady, models.DownloadJobStatusExpired:
			status, attemptStatus, progress = TaskSucceeded, AttemptSucceeded, 10000
		case models.DownloadJobStatusFailed:
			status, attemptStatus, code, message = TaskFailed, AttemptFailed, row.ErrorCode, row.ErrorMessage
		case models.DownloadJobStatusCanceled:
			status, attemptStatus, code, message = TaskCanceled, AttemptCanceled, "canceled", row.ErrorMessage
		case models.DownloadJobStatusPreparing:
			attemptStatus = AttemptInterrupted
			if err := tx.Model(&row).Updates(map[string]any{"status": models.DownloadJobStatusQueued, "progress": 0, "started_at": nil, "finished_at": nil}).Error; err != nil {
				return err
			}
		}
		if err := finishLegacyTask(tx, task, status, attemptStatus, code, message, progress, now); err != nil {
			return err
		}
		if err := tx.Model(&row).Update("background_job_id", job.ID).Error; err != nil {
			return err
		}
		if err := recomputeJob(tx, job, now); err != nil {
			return err
		}
	}
	return nil
}

func backfillUploadJobs(tx *gorm.DB, cutoff, now time.Time) error {
	var rows []models.UploadSession
	if err := tx.Unscoped().Where("status = ? OR (status IN ? AND updated_at >= ?)", models.UploadStatusImporting, []string{models.UploadStatusDone, models.UploadStatusFailed}, cutoff).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		owner := row.UserID
		job, existed, err := legacyJob(tx, Job{ID: uuid.NewString(), Kind: "media.ingest", Status: JobQueued, Visibility: VisibilityUser, OwnerID: &owner, SubjectType: "upload_session", SubjectID: row.TusID, IdempotencyKey: fmt.Sprintf("legacy:upload:%d", row.ID), Label: "Import " + row.Name, CreatedAt: valueTime(row.CreatedAt, now)})
		if err != nil {
			return err
		}
		if existed {
			continue
		}
		task, err := createTask(tx, job.ID, TaskSpec{Kind: "media.import", Queue: QueueStorage, Phase: "Importing upload", Payload: map[string]any{"uploadSessionId": row.ID}, DedupeKey: fmt.Sprintf("upload:%d", row.ID), Priority: 40, Required: true, Weight: 20})
		if err != nil {
			return err
		}
		status, attemptStatus, code, message, progress := TaskQueued, "", "", "", 0
		switch row.Status {
		case models.UploadStatusDone:
			status, attemptStatus, progress = TaskSucceeded, AttemptSucceeded, 10000
		case models.UploadStatusFailed:
			status, attemptStatus, code, message = TaskFailed, AttemptFailed, "legacy_import_failed", boundedMessage(row.Error, 512)
		case models.UploadStatusImporting:
			attemptStatus = AttemptInterrupted
			if err := tx.Model(&row).Update("status", models.UploadStatusUploaded).Error; err != nil {
				return err
			}
		}
		if err := finishLegacyTask(tx, task, status, attemptStatus, code, message, progress, now); err != nil {
			return err
		}
		if err := tx.Model(&row).Update("background_job_id", job.ID).Error; err != nil {
			return err
		}
		if row.LinkID > 0 {
			resultID := fmt.Sprint(row.LinkID)
			var link models.Link
			if err := tx.Unscoped().First(&link, row.LinkID).Error; err == nil && link.UUID != "" {
				resultID = link.UUID
			}
			if err := tx.Model(job).Updates(map[string]any{"result_type": "link", "result_id": resultID}).Error; err != nil {
				return err
			}
		}
		if err := recomputeJob(tx, job, now); err != nil {
			return err
		}
	}
	return nil
}

func legacyJob(tx *gorm.DB, candidate Job) (*Job, bool, error) {
	var existing Job
	if err := tx.Where("idempotency_key = ?", candidate.IdempotencyKey).First(&existing).Error; err == nil {
		return &existing, true, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	if err := tx.Create(&candidate).Error; err != nil {
		return nil, false, err
	}
	if err := addEvent(tx, Event{JobID: candidate.ID, Type: "job_migrated", Message: "Imported from legacy background state"}); err != nil {
		return nil, false, err
	}
	return &candidate, false, nil
}

func finishLegacyTask(tx *gorm.DB, task *Task, status, attemptStatus, code, message string, progress int, now time.Time) error {
	updates := map[string]any{"status": status, "progress": progress, "error_code": boundedMessage(code, 80), "error_message": boundedMessage(message, 512)}
	if terminalTaskStatus(status) {
		updates["finished_at"] = &now
	}
	if attemptStatus != "" {
		updates["attempt_count"] = 1
		attempt := Attempt{ID: uuid.NewString(), TaskID: task.ID, Number: 1, Status: attemptStatus, Worker: "legacy", StartedAt: task.CreatedAt, FinishedAt: &now, ErrorCode: code, ErrorMessage: message}
		if err := tx.Create(&attempt).Error; err != nil {
			return err
		}
	}
	return tx.Model(task).Updates(updates).Error
}

func setEncodingTaskReference(tx *gorm.DB, kind string, id uint, taskID string) error {
	var value any
	switch kind {
	case "quality":
		value = &models.Quality{}
	case "audio":
		value = &models.Audio{}
	case "subtitle":
		value = &models.Subtitle{}
	default:
		return fmt.Errorf("unknown encoding kind %q", kind)
	}
	return tx.Model(value).Where("id = ?", id).Update("background_task_id", taskID).Error
}

func encodingPriority(kind string) int {
	switch kind {
	case "subtitle":
		return 40
	case "audio":
		return 35
	default:
		return 25
	}
}

func clampProgress(value float64) int {
	if value < 0 {
		return 0
	}
	if value > 1 {
		value /= 100
	}
	if value > 1 {
		value = 1
	}
	return int(value * 10000)
}

func valueTime(value *time.Time, fallback time.Time) time.Time {
	if value != nil {
		return *value
	}
	return fallback
}

func safeRemoteLabel(name string) string {
	if name == "" {
		return "Remote download"
	}
	return "Download " + boundedMessage(name, 230)
}
