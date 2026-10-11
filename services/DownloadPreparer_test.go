package services

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	downloadsvc "ch/kirari04/videocms/download"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeDownloadAssembler struct {
	data    []byte
	err     error
	inspect func(*downloadsvc.Selection, func(float64))
}

type timeoutDownloadAssembler struct{}

func (timeoutDownloadAssembler) Assemble(
	ctx context.Context,
	_ *downloadsvc.Selection,
	outputPath string,
	progress func(float64),
) error {
	if err := os.WriteFile(outputPath, []byte("partial"), 0o600); err != nil {
		return err
	}
	if progress != nil {
		progress(0.25)
	}
	<-ctx.Done()
	return ctx.Err()
}

type countingDownloadAssembler struct {
	calls atomic.Int32
}

func (f *countingDownloadAssembler) Assemble(
	ctx context.Context,
	_ *downloadsvc.Selection,
	outputPath string,
	progress func(float64),
) error {
	f.calls.Add(1)
	if progress != nil {
		progress(0.5)
	}
	return os.WriteFile(outputPath, []byte("claimed-once"), 0o600)
}

func (f fakeDownloadAssembler) Assemble(
	ctx context.Context,
	selection *downloadsvc.Selection,
	outputPath string,
	progress func(float64),
) error {
	if f.err != nil {
		return f.err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.inspect != nil {
		f.inspect(selection, progress)
	}
	if progress != nil {
		progress(0.5)
	}
	return os.WriteFile(outputPath, f.data, 0o644)
}

func TestProcessDownloadPreparationCreatesReadyArtifact(t *testing.T) {
	worker, job, root := downloadPreparerTestWorker(t)
	worker.downloadAssembler = fakeDownloadAssembler{data: []byte("prepared")}

	worker.processDownloadPreparation(context.Background(), *job)

	var updated models.DownloadJob
	if err := worker.deps.DB.First(&updated, job.ID).Error; err != nil {
		t.Fatalf("reload job: %v", err)
	}
	if updated.Status != models.DownloadJobStatusReady || updated.Progress != 1 {
		t.Fatalf("updated job = %#v", updated)
	}
	if updated.OutputSize != int64(len("prepared")) || updated.ExpiresAt == nil {
		t.Fatalf("output metadata = %#v", updated)
	}
	if !downloadJobTestPathInside(root, updated.OutputPath) {
		t.Fatalf("output path %q is outside %q", updated.OutputPath, root)
	}
	if data, err := os.ReadFile(updated.OutputPath); err != nil || string(data) != "prepared" {
		t.Fatalf("prepared file = %q, %v", string(data), err)
	}
}

type observedDownloadPreparationStore struct {
	storage.Store
	beforeRead func(context.Context, int64) error
}

func (s observedDownloadPreparationStore) Open(ctx context.Context, key storage.Key) (*storage.Object, error) {
	object, err := s.Store.Open(ctx, key)
	if err == nil {
		object.Body = &observedDownloadPreparationReader{ReadSeekCloser: object.Body, ctx: ctx, beforeRead: s.beforeRead}
	}
	return object, err
}

type observedDownloadPreparationReader struct {
	storage.ReadSeekCloser
	ctx        context.Context
	beforeRead func(context.Context, int64) error
	readBytes  int64
}

func (r *observedDownloadPreparationReader) Read(buffer []byte) (int, error) {
	if r.beforeRead != nil {
		if err := r.beforeRead(r.ctx, r.readBytes); err != nil {
			return 0, err
		}
	}
	count, err := r.ReadSeekCloser.Read(buffer)
	r.readBytes += int64(count)
	return count, err
}

func downloadPreparationTestStorage(t *testing.T, worker *WorkerGroup, job *models.DownloadJob, wrap func(storage.Store) storage.Store) (string, string) {
	t.Helper()
	backing, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var file models.File
	if err := worker.deps.DB.First(&file, job.FileID).Error; err != nil {
		t.Fatal(err)
	}
	key, err := storage.ParseKey(file.UUID + "/720p/out0.ts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backing.Put(context.Background(), key, strings.NewReader(strings.Repeat("v", 256*1024)), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	workspaceRoot := t.TempDir()
	workspace, err := storage.NewLocalWorkspace(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	service, err := storage.NewServiceWithWorkspace("test", storage.LegacyMediaLayout{}, workspace, map[string]storage.Store{"test": wrap(backing)})
	if err != nil {
		t.Fatal(err)
	}
	worker.deps.Storage = service
	t.Cleanup(func() { _ = service.Close() })
	localPath, err := backing.LocalPath(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	return workspaceRoot, localPath
}

func TestProcessDownloadPreparationReportsProgressBeforeRemoteInputsFinish(t *testing.T) {
	worker, job, _ := downloadPreparerTestWorker(t)
	observedPartial := false
	workspaceRoot, _ := downloadPreparationTestStorage(t, worker, job, func(backing storage.Store) storage.Store {
		return observedDownloadPreparationStore{Store: backing, beforeRead: func(_ context.Context, readBytes int64) error {
			if readBytes == 128*1024 {
				var current models.DownloadJob
				if err := worker.deps.DB.First(&current, job.ID).Error; err != nil {
					return err
				}
				if current.Status != models.DownloadJobStatusPreparing || current.Phase != models.DownloadJobPhaseFetching || current.Progress <= 0 || current.Progress >= 0.5 {
					t.Fatalf("job while first object is partially fetched = %#v", current)
				}
				observedPartial = true
			}
			return nil
		}}
	})
	assembled := false
	worker.downloadAssembler = fakeDownloadAssembler{data: []byte("prepared"), inspect: func(selection *downloadsvc.Selection, progress func(float64)) {
		assembled = true
		var current models.DownloadJob
		if err := worker.deps.DB.First(&current, job.ID).Error; err != nil {
			t.Fatal(err)
		}
		if current.Progress != 0.5 || current.Phase != models.DownloadJobPhaseAssembling {
			t.Fatalf("job at assembly start = %#v", current)
		}
		if data, err := os.ReadFile(filepath.Join(selection.Quality.Path, "out0.ts")); err != nil || len(data) != 256*1024 {
			t.Fatalf("assembly input = %d bytes, %v", len(data), err)
		}
		previous := current.Progress
		for _, value := range []float64{0, 0.5, 1} {
			progress(value)
			if err := worker.deps.DB.First(&current, job.ID).Error; err != nil {
				t.Fatal(err)
			}
			if current.Progress < previous || current.Progress >= 1 || current.Status != models.DownloadJobStatusPreparing {
				t.Fatalf("job before output finalization = %#v", current)
			}
			previous = current.Progress
		}
	}}
	worker.processDownloadPreparation(context.Background(), *job)
	if !observedPartial || !assembled {
		t.Fatalf("partial input observed = %v, assembled = %v", observedPartial, assembled)
	}
	var current models.DownloadJob
	if err := worker.deps.DB.First(&current, job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.Status != models.DownloadJobStatusReady || current.Progress != 1 || current.Phase != "" {
		t.Fatalf("completed job = %#v", current)
	}
	if entries, err := os.ReadDir(workspaceRoot); err != nil || len(entries) != 0 {
		t.Fatalf("scratch after completion = %v, %v", entries, err)
	}
}

func TestProcessDownloadPreparationLocalInputsDoNotReserveTransferProgress(t *testing.T) {
	worker, job, _ := downloadPreparerTestWorker(t)
	_, sourcePath := downloadPreparationTestStorage(t, worker, job, func(backing storage.Store) storage.Store { return backing })
	assembled := false
	worker.downloadAssembler = fakeDownloadAssembler{data: []byte("prepared"), inspect: func(_ *downloadsvc.Selection, _ func(float64)) {
		assembled = true
		var current models.DownloadJob
		if err := worker.deps.DB.First(&current, job.ID).Error; err != nil {
			t.Fatal(err)
		}
		if current.Progress != 0 || current.Phase != models.DownloadJobPhaseAssembling {
			t.Fatalf("local job at assembly start = %#v", current)
		}
	}}
	worker.processDownloadPreparation(context.Background(), *job)
	if !assembled {
		t.Fatal("local inputs did not reach assembly")
	}
	if _, err := os.Stat(sourcePath); err != nil {
		t.Fatalf("local input after preparation = %v", err)
	}
}

func TestProcessDownloadPreparationInterruptedFetchCleansInputs(t *testing.T) {
	for _, interruption := range []string{"failed", "timeout", "canceled"} {
		t.Run(interruption, func(t *testing.T) {
			worker, job, _ := downloadPreparerTestWorker(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if interruption == "timeout" {
				worker.preparationTimeout = func(float64) time.Duration { return 20 * time.Millisecond }
			}
			workspaceRoot, _ := downloadPreparationTestStorage(t, worker, job, func(backing storage.Store) storage.Store {
				return observedDownloadPreparationStore{Store: backing, beforeRead: func(readCtx context.Context, readBytes int64) error {
					if readBytes == 0 {
						return nil
					}
					switch interruption {
					case "timeout":
						<-readCtx.Done()
						return readCtx.Err()
					case "canceled":
						worker.cancelDownloadJob(job, "canceled", "Download preparation canceled")
						cancel()
						return readCtx.Err()
					default:
						return errors.New("private storage failure details")
					}
				}}
			})
			worker.downloadAssembler = fakeDownloadAssembler{inspect: func(*downloadsvc.Selection, func(float64)) { t.Fatal("assembly started with incomplete inputs") }}
			worker.processDownloadPreparation(ctx, *job)
			var current models.DownloadJob
			if err := worker.deps.DB.First(&current, job.ID).Error; err != nil {
				t.Fatal(err)
			}
			wantStatus, wantCode := models.DownloadJobStatusFailed, "source_unavailable"
			if interruption == "timeout" {
				wantCode = "preparation_timeout"
			} else if interruption == "canceled" {
				wantStatus, wantCode = models.DownloadJobStatusCanceled, "canceled"
			}
			if current.Status != wantStatus || current.ErrorCode != wantCode || strings.Contains(current.ErrorMessage, "private") {
				t.Fatalf("interrupted fetch job = %#v", current)
			}
			if entries, err := os.ReadDir(workspaceRoot); err != nil || len(entries) != 0 {
				t.Fatalf("scratch after interruption = %v, %v", entries, err)
			}
		})
	}
}

func TestDownloadPreparationCleanupExpiresArtifact(t *testing.T) {
	worker, job, _ := downloadPreparerTestWorker(t)
	outputDir := worker.downloadJobDirectory()
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	outputPath := filepath.Join(outputDir, job.UUID+".mkv")
	if err := os.WriteFile(outputPath, []byte("expired"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	past := time.Now().Add(-time.Minute)
	if err := worker.deps.DB.Model(job).Updates(map[string]interface{}{
		"status":      models.DownloadJobStatusReady,
		"progress":    1,
		"output_path": outputPath,
		"output_size": 7,
		"expires_at":  &past,
	}).Error; err != nil {
		t.Fatalf("mark ready: %v", err)
	}

	worker.runDownloadPreparationCleanup()

	var updated models.DownloadJob
	if err := worker.deps.DB.First(&updated, job.ID).Error; err != nil {
		t.Fatalf("reload job: %v", err)
	}
	if updated.Status != models.DownloadJobStatusExpired || updated.OutputPath != "" {
		t.Fatalf("updated job = %#v", updated)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("expired artifact still exists: %v", err)
	}
}

func TestRecoverDownloadPreparationsRequeuesAndRemovesPartial(t *testing.T) {
	worker, job, _ := downloadPreparerTestWorker(t)
	outputDir := worker.downloadJobDirectory()
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	partPath := filepath.Join(outputDir, job.UUID+".mkv.part")
	if err := os.WriteFile(partPath, []byte("partial"), 0o644); err != nil {
		t.Fatalf("write part: %v", err)
	}

	worker.recoverDownloadPreparations()

	var updated models.DownloadJob
	if err := worker.deps.DB.First(&updated, job.ID).Error; err != nil {
		t.Fatalf("reload job: %v", err)
	}
	if updated.Status != models.DownloadJobStatusQueued || updated.StartedAt != nil || updated.Progress != 0 {
		t.Fatalf("updated job = %#v", updated)
	}
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatalf("partial file still exists: %v", err)
	}
}

func TestProcessDownloadPreparationFailureRemovesPartial(t *testing.T) {
	worker, job, _ := downloadPreparerTestWorker(t)
	worker.downloadAssembler = fakeDownloadAssembler{err: errors.New("assembler failed")}

	worker.processDownloadPreparation(context.Background(), *job)

	var updated models.DownloadJob
	if err := worker.deps.DB.First(&updated, job.ID).Error; err != nil {
		t.Fatalf("reload job: %v", err)
	}
	if updated.Status != models.DownloadJobStatusFailed || updated.ErrorCode != "preparation_failed" {
		t.Fatalf("updated job = %#v", updated)
	}
	partPath := filepath.Join(worker.downloadJobDirectory(), job.UUID+".mkv.part")
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatalf("partial file still exists: %v", err)
	}
}

func TestProcessDownloadPreparationTimeoutIsSanitizedAndRemovesPartial(t *testing.T) {
	worker, job, _ := downloadPreparerTestWorker(t)
	worker.downloadAssembler = timeoutDownloadAssembler{}
	worker.preparationTimeout = func(float64) time.Duration { return 20 * time.Millisecond }

	worker.processDownloadPreparation(context.Background(), *job)

	var updated models.DownloadJob
	if err := worker.deps.DB.First(&updated, job.ID).Error; err != nil {
		t.Fatalf("reload job: %v", err)
	}
	if updated.Status != models.DownloadJobStatusFailed ||
		updated.ErrorCode != "preparation_timeout" ||
		updated.ErrorMessage != "Preparing this download took too long." {
		t.Fatalf("updated timeout job = %#v", updated)
	}
	partPath := filepath.Join(worker.downloadJobDirectory(), job.UUID+".mkv.part")
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatalf("partial file still exists: %v", err)
	}
}

func TestAtomicDownloadPreparationClaimRunsJobOnce(t *testing.T) {
	firstWorker, job, _ := downloadPreparerTestWorker(t)
	if err := firstWorker.deps.DB.Model(job).Updates(map[string]interface{}{
		"status":     models.DownloadJobStatusQueued,
		"started_at": nil,
	}).Error; err != nil {
		t.Fatalf("queue job: %v", err)
	}
	assembler := &countingDownloadAssembler{}
	firstWorker.downloadAssembler = assembler
	secondWorker := NewWorkerGroup(firstWorker.deps, firstWorker.logic)
	secondWorker.downloadAssembler = assembler

	firstWorker.loadDownloadPreparationTasks(context.Background())
	secondWorker.loadDownloadPreparationTasks(context.Background())

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var updated models.DownloadJob
		if err := firstWorker.deps.DB.First(&updated, job.ID).Error; err == nil &&
			updated.Status == models.DownloadJobStatusReady {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	var updated models.DownloadJob
	if err := firstWorker.deps.DB.First(&updated, job.ID).Error; err != nil {
		t.Fatalf("reload job: %v", err)
	}
	if updated.Status != models.DownloadJobStatusReady {
		t.Fatalf("job status = %q, want ready", updated.Status)
	}
	if calls := assembler.calls.Load(); calls != 1 {
		t.Fatalf("assembler calls = %d, want 1", calls)
	}
}

func TestDownloadPreparationCleanupRemovesOnlySafeOrphans(t *testing.T) {
	worker, _, _ := downloadPreparerTestWorker(t)
	outputDir := worker.downloadJobDirectory()
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}
	orphanPath := filepath.Join(outputDir, "orphan.mkv")
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(orphanPath, old, old); err != nil {
		t.Fatalf("age orphan: %v", err)
	}

	outsidePath := filepath.Join(t.TempDir(), "keep.mkv")
	if err := os.WriteFile(outsidePath, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if _, removed := worker.removeDownloadJobPath(outsidePath); removed {
		t.Fatal("unsafe outside path was removed")
	}

	worker.runDownloadPreparationCleanup()

	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Fatalf("old orphan still exists: %v", err)
	}
	if data, err := os.ReadFile(outsidePath); err != nil || string(data) != "keep" {
		t.Fatalf("outside file changed: %q, %v", string(data), err)
	}
}

func TestCancelAllDownloadPreparationsRevokesReadyArtifact(t *testing.T) {
	worker, job, _ := downloadPreparerTestWorker(t)
	outputDir := worker.downloadJobDirectory()
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}
	outputPath := filepath.Join(outputDir, job.UUID+".mkv")
	if err := os.WriteFile(outputPath, []byte("ready"), 0o600); err != nil {
		t.Fatalf("write ready artifact: %v", err)
	}
	expires := time.Now().Add(time.Hour)
	if err := worker.deps.DB.Model(job).Updates(map[string]interface{}{
		"status":      models.DownloadJobStatusReady,
		"output_path": outputPath,
		"output_size": 5,
		"expires_at":  &expires,
	}).Error; err != nil {
		t.Fatalf("mark ready: %v", err)
	}

	worker.CancelAllDownloadPreparations("Downloads disabled by administrator")

	var updated models.DownloadJob
	if err := worker.deps.DB.First(&updated, job.ID).Error; err != nil {
		t.Fatalf("reload job: %v", err)
	}
	if updated.Status != models.DownloadJobStatusCanceled ||
		updated.ErrorCode != "downloads_disabled" ||
		updated.OutputPath != "" {
		t.Fatalf("canceled job = %#v", updated)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("revoked artifact still exists: %v", err)
	}
}

func downloadPreparerTestWorker(t *testing.T) (*WorkerGroup, *models.DownloadJob, string) {
	t.Helper()
	db, err := gorm.Open(
		sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"),
		&gorm.Config{},
	)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.File{},
		&models.Link{},
		&models.Quality{},
		&models.Audio{},
		&models.Subtitle{},
		&models.DownloadJob{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	root := t.TempDir()
	enabled := true
	deps := &app.Deps{
		DB: db,
		Snapshots: app.NewSnapshotStore(app.Snapshot{Config: config.Config{
			FolderVideoUploadsPriv:            root,
			DownloadEnabled:                   &enabled,
			MaxParallelDownloadPreparations:   1,
			MaxQueuedDownloadPreparations:     20,
			DownloadPreparationRetentionHours: 6,
		}}),
	}
	file := models.File{
		UUID:     "550e8400-e29b-41d4-a716-446655440200",
		Duration: 120,
		UserID:   1,
	}
	if err := db.Create(&file).Error; err != nil {
		t.Fatalf("create file: %v", err)
	}
	quality := models.Quality{
		FileID:     file.ID,
		Name:       "720p",
		Type:       "hls",
		Ready:      true,
		Path:       filepath.Join(root, "source"),
		OutputFile: "out.m3u8",
	}
	if err := db.Create(&quality).Error; err != nil {
		t.Fatalf("create quality: %v", err)
	}
	link := models.Link{
		UUID:   "550e8400-e29b-41d4-a716-446655440201",
		Name:   "Worker Test",
		FileID: file.ID,
		UserID: 2,
	}
	if err := db.Create(&link).Error; err != nil {
		t.Fatalf("create link: %v", err)
	}
	started := time.Now()
	job := models.DownloadJob{
		UUID:          "550e8400-e29b-41d4-a716-446655440202",
		LinkID:        link.ID,
		LinkUUID:      link.UUID,
		FileID:        file.ID,
		UserID:        link.UserID,
		QualityID:     quality.ID,
		QualityName:   quality.Name,
		Container:     downloadsvc.ContainerMKV,
		AudioUUIDs:    "[]",
		SubtitleUUIDs: "[]",
		MediaDuration: file.Duration,
		Status:        models.DownloadJobStatusPreparing,
		StartedAt:     &started,
		OutputName:    "worker-test[720p].mkv",
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}
	worker := NewWorkerGroup(deps, logic.NewService(deps))
	return worker, &job, root
}

func downloadJobTestPathInside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
