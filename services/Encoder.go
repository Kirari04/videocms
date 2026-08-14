package services

import (
	"ch/kirari04/videocms/background"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/models"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/imroc/req/v3"
	"gorm.io/gorm"
)

type ActiveEncoding struct {
	Task   EncodingTask
	Cancel context.CancelFunc
}

type EncodingTask struct {
	Type      string
	FileID    uint
	FileUUID  string
	StorageID string
	ID        uint // qualityID | audioID | subID
	Name      string
}

type IwithProcess interface {
	SetProcess(float64)
	GetProcess() float64
	Save(DB *gorm.DB) *gorm.DB
}

func (w *WorkerGroup) Encoder(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg := w.Config()
	w.encoderLogf(
		"scheduler_started",
		"enabled=%t max_concurrent=%d",
		encodingEnabled(cfg),
		maxRunningEncodes(cfg),
	)

	for {
		if encodingEnabled(w.Config()) {
			w.loadEncodingTasks(ctx)
		}

		interval := w.encoderPollInterval
		if interval <= 0 {
			interval = time.Second * 10
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-w.encoderConfigChanged:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// NotifyEncoderConfigChanged applies EncodingEnabled and MaxRunningEncodes
// changes immediately. In-flight encodes are allowed to finish; disabling the
// encoder only prevents new work from being claimed.
func (w *WorkerGroup) NotifyEncoderConfigChanged() {
	cfg := w.Config()
	w.encoderLogf(
		"configuration_updated",
		"enabled=%t max_concurrent=%d",
		encodingEnabled(cfg),
		maxRunningEncodes(cfg),
	)
	w.wakeEncoder()
}

func encodingEnabled(cfg config.Config) bool {
	return cfg.EncodingEnabled != nil && *cfg.EncodingEnabled
}

func maxRunningEncodes(cfg config.Config) int {
	if cfg.MaxRunningEncodes < 1 {
		return 1
	}
	return int(cfg.MaxRunningEncodes)
}

func (w *WorkerGroup) wakeEncoder() {
	if w.encoderConfigChanged == nil {
		return
	}
	select {
	case w.encoderConfigChanged <- struct{}{}:
	default:
	}
}

func (w *WorkerGroup) availableEncodingSlots() int {
	w.encoderMu.Lock()
	defer w.encoderMu.Unlock()

	cfg := w.Config()
	if !encodingEnabled(cfg) {
		return 0
	}
	available := maxRunningEncodes(cfg) - w.activeEncodingJobs
	if available < 0 {
		return 0
	}
	return available
}

func (w *WorkerGroup) tryReserveEncodingSlot() bool {
	w.encoderMu.Lock()
	defer w.encoderMu.Unlock()

	cfg := w.Config()
	if !encodingEnabled(cfg) || w.activeEncodingJobs >= maxRunningEncodes(cfg) {
		return false
	}
	w.activeEncodingJobs++
	return true
}

func (w *WorkerGroup) releaseEncodingSlot() {
	w.encoderMu.Lock()
	if w.activeEncodingJobs > 0 {
		w.activeEncodingJobs--
	}
	w.encoderMu.Unlock()
	w.wakeEncoder()
}

func (w *WorkerGroup) ResetEncodingState() {
	if res := w.deps.DB.
		Model(&models.Quality{}).
		Where(&models.Quality{
			Encoding: true,
		}, "Encoding").
		Or("progress > ?", 0).
		Updates(map[string]interface{}{"encoding": false, "progress": 0}); res.Error != nil {
		w.encoderLogf("state_reset_failed", "task_type=quality error=%q", res.Error)
	}

	if res := w.deps.DB.
		Model(&models.Audio{}).
		Where(&models.Audio{
			Encoding: true,
		}, "Encoding").
		Or("progress > ?", 0).
		Updates(map[string]interface{}{"encoding": false, "progress": 0}); res.Error != nil {
		w.encoderLogf("state_reset_failed", "task_type=audio error=%q", res.Error)
	}

	if res := w.deps.DB.
		Model(&models.Subtitle{}).
		Where(&models.Subtitle{
			Encoding: true,
		}, "Encoding").
		Or("progress > ?", 0).
		Updates(map[string]interface{}{"encoding": false, "progress": 0}); res.Error != nil {
		w.encoderLogf("state_reset_failed", "task_type=subtitle error=%q", res.Error)
	}
}

func (w *WorkerGroup) loadEncodingTasks(ctx context.Context) {
	available := w.availableEncodingSlots()
	if available == 0 {
		return
	}
	if available > 10 {
		available = 10
	}

	// we want to encode the subtitles first, then audio and in the end the qualities
	// SUBTITLES
	var encodingSubs []models.Subtitle
	result := w.deps.DB.
		Model(&models.Subtitle{}).
		Joins("JOIN files ON files.id = subtitles.file_id").
		Preload("File").
		Where("files.storage_state = ?", models.FileStorageAvailable).
		Where(&models.Subtitle{
			Encoding: false,
			Ready:    false,
			Failed:   false,
		}, "Encoding", "Ready", "Failed").
		Order("subtitles.id ASC").
		Limit(available).
		Find(&encodingSubs)
	if result.Error != nil {
		w.encoderLogf("queue_load_failed", "task_type=subtitle error=%q", result.Error)
		return
	}

	if len(encodingSubs) > 0 {
		w.encoderLogf("queue_loaded", "task_type=subtitle count=%d", len(encodingSubs))
	}

	for i := range encodingSubs {
		v := &encodingSubs[i]
		if !w.tryReserveEncodingSlot() {
			return
		}
		task := EncodingTask{
			Type:      "subtitle",
			FileID:    v.FileID,
			FileUUID:  v.File.UUID,
			StorageID: v.File.StorageID,
			ID:        v.ID,
			Name:      v.Name,
		}
		v.Encoding = true
		if result := v.Save(w.deps.DB); result.Error != nil {
			w.releaseEncodingSlot()
			w.encodingTaskLog("task_claim_failed", task, time.Time{}, result.Error)
			continue
		}
		w.startEncodingTask(ctx, task)
		available--
	}

	// AUDIOS
	var encodingAudios []models.Audio
	if available > 0 {
		result = w.deps.DB.
			Model(&models.Audio{}).
			Joins("JOIN files ON files.id = audios.file_id").
			Preload("File").
			Where("files.storage_state = ?", models.FileStorageAvailable).
			Where(&models.Audio{
				Encoding: false,
				Ready:    false,
				Failed:   false,
			}, "Encoding", "Ready", "Failed").
			Order("audios.id ASC").
			Limit(available).
			Find(&encodingAudios)
		if result.Error != nil {
			w.encoderLogf("queue_load_failed", "task_type=audio error=%q", result.Error)
			return
		}
	}

	if len(encodingAudios) > 0 {
		w.encoderLogf("queue_loaded", "task_type=audio count=%d", len(encodingAudios))
	}

	for i := range encodingAudios {
		v := &encodingAudios[i]
		if !w.tryReserveEncodingSlot() {
			return
		}
		task := EncodingTask{
			Type:      "audio",
			FileID:    v.FileID,
			FileUUID:  v.File.UUID,
			StorageID: v.File.StorageID,
			ID:        v.ID,
			Name:      v.Name,
		}
		v.Encoding = true
		if result := v.Save(w.deps.DB); result.Error != nil {
			w.releaseEncodingSlot()
			w.encodingTaskLog("task_claim_failed", task, time.Time{}, result.Error)
			continue
		}
		w.startEncodingTask(ctx, task)
		available--
	}

	// QUALITYS
	var encodingQualitys []models.Quality
	if available > 0 {
		result = w.deps.DB.
			Model(&models.Quality{}).
			Joins("JOIN files ON files.id = qualities.file_id").
			Preload("File").
			Where("files.storage_state = ?", models.FileStorageAvailable).
			Where(&models.Quality{
				Encoding: false,
				Ready:    false,
				Failed:   false,
			}, "Encoding", "Ready", "Failed").
			Order("qualities.id ASC").
			Limit(available).
			Find(&encodingQualitys)
		if result.Error != nil {
			w.encoderLogf("queue_load_failed", "task_type=quality error=%q", result.Error)
			return
		}
	}

	if len(encodingQualitys) > 0 {
		w.encoderLogf("queue_loaded", "task_type=quality count=%d", len(encodingQualitys))
	}

	for i := range encodingQualitys {
		v := &encodingQualitys[i]
		if !w.tryReserveEncodingSlot() {
			return
		}
		task := EncodingTask{
			Type:      "quality",
			FileID:    v.FileID,
			FileUUID:  v.File.UUID,
			StorageID: v.File.StorageID,
			ID:        v.ID,
			Name:      v.Name,
		}
		v.Encoding = true
		if result := v.Save(w.deps.DB); result.Error != nil {
			w.releaseEncodingSlot()
			w.encodingTaskLog("task_claim_failed", task, time.Time{}, result.Error)
			continue
		}
		w.startEncodingTask(ctx, task)
		available--
	}
}

func (w *WorkerGroup) startEncodingTask(ctx context.Context, task EncodingTask) {
	go func() {
		defer w.releaseEncodingSlot()
		taskCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		w.addActiveEncoding(ActiveEncoding{Task: task, Cancel: cancel})
		defer w.deleteActiveEncoding(task)

		started := time.Now()
		w.encodingTaskLog("task_started", task, time.Time{}, nil)

		var err error
		if w.encodingTaskRunner != nil {
			err = w.encodingTaskRunner(taskCtx, task)
		} else {
			err = w.runEncode(taskCtx, task)
		}
		if err != nil {
			w.encodingTaskLog("task_failed", task, started, err)
			return
		}
		w.encodingTaskLog("task_completed", task, started, nil)
	}()
}

func (w *WorkerGroup) runEncode(ctx context.Context, encodingTaskInformation EncodingTask) error {
	switch encodingTaskInformation.Type {
	case "quality":
		var encodingTask models.Quality
		if result := w.deps.DB.Preload("File").First(&encodingTask, encodingTaskInformation.ID); result.Error != nil {
			return fmt.Errorf("load quality task: %w", result.Error)
		}
		return w.runEncodeQuality(ctx, encodingTask, encodingTaskInformation)
	case "audio":
		var encodingTask models.Audio
		if result := w.deps.DB.Preload("File").First(&encodingTask, encodingTaskInformation.ID); result.Error != nil {
			return fmt.Errorf("load audio task: %w", result.Error)
		}
		return w.runEncodeAudio(ctx, encodingTask, encodingTaskInformation)
	case "subtitle":
		var encodingTask models.Subtitle
		if result := w.deps.DB.Preload("File").First(&encodingTask, encodingTaskInformation.ID); result.Error != nil {
			return fmt.Errorf("load subtitle task: %w", result.Error)
		}
		return w.runEncodeSub(ctx, encodingTask, encodingTaskInformation)
	default:
		return fmt.Errorf("unsupported encoding task type %q", encodingTaskInformation.Type)
	}
}

func (w *WorkerGroup) runEncodeQuality(ctx context.Context, encodingTask models.Quality, task EncodingTask) error {
	// we check if the original file has been deleted during the waittime
	exists, err := w.originalFileExists(encodingTask.FileID)
	if err != nil {
		return w.failEncodingTask(&encodingTask, fmt.Errorf("check source file: %w", err))
	}
	if !exists {
		return w.failEncodingTask(&encodingTask, fmt.Errorf("source file was deleted before encoding started"))
	}

	totalDuration := encodingTask.File.Duration
	absFileInput, cleanupInput, err := w.materializeEncodingSource(ctx, encodingTask.File)
	if err != nil {
		return w.failEncodingTask(&encodingTask, fmt.Errorf("prepare source: %w", err))
	}
	defer w.cleanupEncodingResource(task, "source", cleanupInput)
	absFolderOutput, cleanupOutput, err := w.encodingOutputDirectory(ctx, "encode-quality", encodingTask.Path)
	if err != nil {
		return w.failEncodingTask(&encodingTask, fmt.Errorf("prepare output workspace: %w", err))
	}
	defer w.cleanupEncodingResource(task, "output workspace", cleanupOutput)

	var frameRateString string
	var segmenDuration int = 4
	if encodingTask.AvgFrameRate > 0 {
		frameRateString = fmt.Sprintf("-r %.4f", encodingTask.AvgFrameRate)
	}

	encFilePath := fmt.Sprintf("%s/%s", absFolderOutput, encodingTask.OutputFile)

	var ffmpegCommand string
	switch encodingTask.Type {
	case "hls":

		ffmpegCommand = "ffmpeg " +
			fmt.Sprintf("-i %s ", absFileInput) + // input file
			fmt.Sprint("-sn ") + // disable subtitle
			fmt.Sprint("-an ") + // disable audio
			fmt.Sprint("-c:v libx264 ") + // setting video codec libx264
			fmt.Sprintf("-profile:v %s ", encodingTask.Profile) +
			fmt.Sprintf("-level:v %s ", encodingTask.Level) +
			fmt.Sprint("-pix_fmt yuv420p ") + // YUV 4:2:0
			fmt.Sprintf("-crf %d ", encodingTask.Crf) + // setting crf
			fmt.Sprintf("-maxrate %s ", encodingTask.VideoBitrate) + // setting max video bitrate
			fmt.Sprintf("-bufsize %sk ", strconv.Itoa(extractNumber(encodingTask.VideoBitrate)*2)) + // setting video bufsize
			fmt.Sprintf("%s ", frameRateString) + // (optional) setting framerate
			fmt.Sprintf("-force_key_frames \"expr:gte(t,n_forced*%d)\" ", segmenDuration) + // force keyframes every segmentDuration
			"-flags +cgop " + // closed GOP
			fmt.Sprintf("-s %dx%d ", encodingTask.Width, encodingTask.Height) + // setting resolution
			fmt.Sprint("-sc_threshold 0 ") +
			"-f hls " + // hls playlist
			fmt.Sprintf("-hls_time %d ", segmenDuration) + // segment duration
			fmt.Sprint("-hls_playlist_type vod ") +
			fmt.Sprint("-hls_segment_type mpegts ") +
			fmt.Sprint("-hls_list_size 0 ") +
			"-hls_flags independent_segments " + // signals that segments can be decoded independently
			fmt.Sprint("-start_number 0 ") + // start number
			fmt.Sprintf("%s ", encFilePath) + // output file
			fmt.Sprintf("-progress unix://%s -y", w.tempSock(ctx,
				totalDuration,
				fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
				&encodingTask,
				task,
			)) // progress tracking
	default:
		return w.failEncodingTask(
			&encodingTask,
			fmt.Errorf("unsupported quality encoding type %q", encodingTask.Type),
		)
	}

	cmd := exec.CommandContext(ctx,
		"bash",
		"-c",
		ffmpegCommand)
	diagnostics := newEncodingDiagnosticTail(8192)
	cmd.Stdout = diagnostics
	cmd.Stderr = diagnostics

	start := time.Now()
	if err := cmd.Run(); err != nil {
		return w.failEncodingTask(&encodingTask, ffmpegEncodingError(ctx, err, diagnostics))
	}
	if w.deps.Storage != nil && w.deps.Storage.Layout() != nil {
		prefix, err := w.deps.Storage.Layout().VideoPrefix(encodingTask.File.UUID, encodingTask.Name)
		if err == nil {
			err = w.publishEncodingOutput(ctx, encodingTask.File, prefix, absFolderOutput)
		}
		if err != nil {
			return w.failEncodingTask(&encodingTask, fmt.Errorf("publish output: %w", err))
		}
	}
	duration := time.Since(start).Seconds()
	w.logic.TrackEncoding(encodingTask.File.UserID, encodingTask.FileID, "quality", duration)

	qualitySize, err := dirSize(absFolderOutput)
	if err != nil {
		w.encodingTaskLog("output_size_failed", task, time.Time{}, err)
	}

	encodingTask.Size = qualitySize
	return w.completeEncodingTask(&encodingTask)
}

func (w *WorkerGroup) runEncodeAudio(ctx context.Context, encodingTask models.Audio, task EncodingTask) error {
	// we check if the original file has been deleted during the waittime
	exists, err := w.originalFileExists(encodingTask.FileID)
	if err != nil {
		return w.failEncodingTask(&encodingTask, fmt.Errorf("check source file: %w", err))
	}
	if !exists {
		return w.failEncodingTask(&encodingTask, fmt.Errorf("source file was deleted before encoding started"))
	}

	totalDuration := encodingTask.File.Duration
	absFileInput, cleanupInput, err := w.materializeEncodingSource(ctx, encodingTask.File)
	if err != nil {
		return w.failEncodingTask(&encodingTask, fmt.Errorf("prepare source: %w", err))
	}
	defer w.cleanupEncodingResource(task, "source", cleanupInput)
	absFolderOutput, cleanupOutput, err := w.encodingOutputDirectory(ctx, "encode-audio", encodingTask.Path)
	if err != nil {
		return w.failEncodingTask(&encodingTask, fmt.Errorf("prepare output workspace: %w", err))
	}
	defer w.cleanupEncodingResource(task, "output workspace", cleanupOutput)

	var ffmpegCommand string
	switch encodingTask.Type {
	case "hls":

		segmenDuration := 4

		ffmpegCommand = "ffmpeg " +
			fmt.Sprintf("-i %s ", absFileInput) + // input file
			"-sn " + // disable subtitle
			"-vn " + // disable video stream
			fmt.Sprintf("-map 0:a:%d ", encodingTask.Index) + // mapping first audio stream
			`-af aformat=channel_layouts="7.1|5.1|stereo" ` +
			fmt.Sprintf("-c:a %s ", encodingTask.Codec) + // setting audio codec
			"-f hls " + // hls playlist
			fmt.Sprintf("-hls_time %d ", segmenDuration) + // segment duration
			fmt.Sprint("-hls_playlist_type vod ") +
			fmt.Sprint("-hls_segment_type mpegts ") +
			fmt.Sprint("-hls_list_size 0 ") +
			fmt.Sprint("-start_number 0 ") + // start number
			fmt.Sprintf("%s/%s ", absFolderOutput, encodingTask.OutputFile) + // output file
			fmt.Sprintf("-progress unix://%s -y", w.tempSock(ctx,
				totalDuration,
				fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
				&encodingTask,
				task,
			)) // progress tracking
	default:
		return w.failEncodingTask(
			&encodingTask,
			fmt.Errorf("unsupported audio encoding type %q", encodingTask.Type),
		)
	}

	cmd := exec.CommandContext(ctx,
		"bash",
		"-c",
		ffmpegCommand)
	diagnostics := newEncodingDiagnosticTail(8192)
	cmd.Stdout = diagnostics
	cmd.Stderr = diagnostics

	start := time.Now()
	if err := cmd.Run(); err != nil {
		return w.failEncodingTask(&encodingTask, ffmpegEncodingError(ctx, err, diagnostics))
	}
	if w.deps.Storage != nil && w.deps.Storage.Layout() != nil {
		prefix, err := w.deps.Storage.Layout().AudioPrefix(encodingTask.File.UUID, encodingTask.UUID)
		if err == nil {
			err = w.publishEncodingOutput(ctx, encodingTask.File, prefix, absFolderOutput)
		}
		if err != nil {
			return w.failEncodingTask(&encodingTask, fmt.Errorf("publish output: %w", err))
		}
	}
	duration := time.Since(start).Seconds()
	w.logic.TrackEncoding(encodingTask.File.UserID, encodingTask.FileID, "audio", duration)

	return w.completeEncodingTask(&encodingTask)
}

func (w *WorkerGroup) runEncodeSub(ctx context.Context, encodingTask models.Subtitle, task EncodingTask) error {
	// we check if the original file has been deleted during the waittime
	exists, err := w.originalFileExists(encodingTask.FileID)
	if err != nil {
		return w.failEncodingTask(&encodingTask, fmt.Errorf("check source file: %w", err))
	}
	if !exists {
		return w.failEncodingTask(&encodingTask, fmt.Errorf("source file was deleted before encoding started"))
	}

	totalDuration := encodingTask.File.Duration
	absFileInput, cleanupInput, err := w.materializeEncodingSource(ctx, encodingTask.File)
	if err != nil {
		return w.failEncodingTask(&encodingTask, fmt.Errorf("prepare source: %w", err))
	}
	defer w.cleanupEncodingResource(task, "source", cleanupInput)
	absFolderOutput, cleanupOutput, err := w.encodingOutputDirectory(ctx, "encode-subtitle", encodingTask.Path)
	if err != nil {
		return w.failEncodingTask(&encodingTask, fmt.Errorf("prepare output workspace: %w", err))
	}
	defer w.cleanupEncodingResource(task, "output workspace", cleanupOutput)

	var ffmpegCommand string

	if encodingTask.OriginalCodec == "hdmv_pgs_subtitle" {
		cfg := w.Config()
		if cfg.EnablePluginPgsServer == nil || *cfg.EnablePluginPgsServer == false {
			return w.failEncodingTask(
				&encodingTask,
				fmt.Errorf("PGS subtitle preprocessing is disabled"),
			)
		}

		// prepocess pgs
		if err := w.prepocessPgs(ctx, encodingTask, absFolderOutput, &absFileInput); err != nil {
			return w.failEncodingTask(&encodingTask, fmt.Errorf("preprocess PGS subtitle: %w", err))
		}
		preprocessedInput := absFileInput
		defer w.cleanupEncodingResource(task, "preprocessed subtitle", func() error {
			return os.Remove(preprocessedInput)
		})

		switch encodingTask.Type {
		case "ass":
			ffmpegCommand = "ffmpeg " +
				fmt.Sprintf("-i %s ", absFileInput) + // input file
				fmt.Sprintf("-c:s %s ", encodingTask.Codec) + // setting audio codec
				fmt.Sprintf("%s/%s ", absFolderOutput, encodingTask.OutputFile) + // output file
				fmt.Sprintf("-progress unix://%s -y", w.tempSock(ctx,
					totalDuration,
					fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
					&encodingTask,
					task,
				)) // progress tracking
		case "vtt":
			ffmpegCommand = "ffmpeg " +
				fmt.Sprintf("-i %s ", absFileInput) + // input file
				fmt.Sprintf("-c:s %s ", encodingTask.Codec) + // setting audio codec
				fmt.Sprintf("%s/%s ", absFolderOutput, encodingTask.OutputFile) + // output file
				fmt.Sprintf("-progress unix://%s -y", w.tempSock(ctx,
					totalDuration,
					fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
					&encodingTask,
					task,
				)) // progress tracking
		default:
			return w.failEncodingTask(
				&encodingTask,
				fmt.Errorf("unsupported subtitle encoding type %q", encodingTask.Type),
			)
		}
	} else {
		// normal subtitles
		switch encodingTask.Type {
		case "ass":
			ffmpegCommand = "ffmpeg " +
				fmt.Sprintf("-i %s ", absFileInput) + // input file
				"-an " + // disable audio
				"-vn " + // disable video stream
				fmt.Sprintf("-map 0:s:%d ", encodingTask.Index) + // mapping first audio stream
				fmt.Sprintf("-c:s %s ", encodingTask.Codec) + // setting audio codec
				fmt.Sprintf("%s/%s ", absFolderOutput, encodingTask.OutputFile) + // output file
				fmt.Sprintf("-progress unix://%s -y", w.tempSock(ctx,
					totalDuration,
					fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
					&encodingTask,
					task,
				)) // progress tracking
		case "vtt":
			ffmpegCommand = "ffmpeg " +
				fmt.Sprintf("-i %s ", absFileInput) + // input file
				"-an " + // disable audio
				"-vn " + // disable video stream
				fmt.Sprintf("-map 0:s:%d ", encodingTask.Index) + // mapping first audio stream
				fmt.Sprintf("-c:s %s ", encodingTask.Codec) + // setting audio codec
				fmt.Sprintf("%s/%s ", absFolderOutput, encodingTask.OutputFile) + // output file
				fmt.Sprintf("-progress unix://%s -y", w.tempSock(ctx,
					totalDuration,
					fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
					&encodingTask,
					task,
				)) // progress tracking
		default:
			return w.failEncodingTask(
				&encodingTask,
				fmt.Errorf("unsupported subtitle encoding type %q", encodingTask.Type),
			)
		}
	}

	cmd := exec.CommandContext(ctx,
		"bash",
		"-c",
		ffmpegCommand)
	diagnostics := newEncodingDiagnosticTail(8192)
	cmd.Stdout = diagnostics
	cmd.Stderr = diagnostics

	start := time.Now()
	if err := cmd.Run(); err != nil {
		return w.failEncodingTask(&encodingTask, ffmpegEncodingError(ctx, err, diagnostics))
	}
	if w.deps.Storage != nil && w.deps.Storage.Layout() != nil {
		prefix, err := w.deps.Storage.Layout().SubtitlePrefix(encodingTask.File.UUID, encodingTask.UUID)
		if err == nil {
			err = w.publishEncodingOutput(ctx, encodingTask.File, prefix, absFolderOutput)
		}
		if err != nil {
			return w.failEncodingTask(&encodingTask, fmt.Errorf("publish output: %w", err))
		}
	}
	duration := time.Since(start).Seconds()
	w.logic.TrackEncoding(encodingTask.File.UserID, encodingTask.FileID, "sub", duration)

	return w.completeEncodingTask(&encodingTask)
}

func (w *WorkerGroup) tempSock(ctx context.Context, totalDuration float64, sockFileName string, encodingTask IwithProcess, task EncodingTask) string {
	sockFilePath := path.Join(os.TempDir(), sockFileName)
	if err := os.Remove(sockFilePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		w.encodingTaskLog("progress_socket_cleanup_failed", task, time.Time{}, err)
	}
	l, err := net.Listen("unix", sockFilePath)
	if err != nil {
		w.encodingTaskLog("progress_socket_listen_failed", task, time.Time{}, err)
		return sockFilePath
	}

	go func() {
		defer l.Close()
		go func() {
			<-ctx.Done()
			_ = l.Close()
		}()
		re := regexp.MustCompile(`out_time_ms=(\d+)`)
		fd, err := l.Accept()
		if err != nil {
			w.encodingTaskLog("progress_socket_accept_failed", task, time.Time{}, err)
			return
		}
		defer fd.Close()
		buf := make([]byte, 16)
		data := ""
		progress := ""
		for {
			n, err := fd.Read(buf)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					w.encodingTaskLog("progress_socket_read_failed", task, time.Time{}, err)
				}
				return
			}
			data += string(buf[:n])
			if len(data) > 4096 {
				data = data[len(data)-4096:]
			}
			a := re.FindAllStringSubmatch(data, -1)
			cp := ""
			if len(a) > 0 && len(a[len(a)-1]) > 0 {
				c, _ := strconv.Atoi(a[len(a)-1][len(a[len(a)-1])-1])
				cp = fmt.Sprintf("%.2f", float64(c)/totalDuration/1000000)
			}
			if strings.Contains(data, "progress=end") {
				cp = "1.0"
			}
			if cp == "" {
				cp = ".0"
			}
			if cp != progress {
				progress = cp
				// fmt.Println("progress: ", progress)
				floatProg, err := strconv.ParseFloat(progress, 64)
				if err != nil {
					w.encodingTaskLog("progress_parse_failed", task, time.Time{}, err)
					continue
				}
				if floatProg != 0 {
					encodingTask.SetProcess(floatProg)
					background.ReportProgress(ctx, floatProg, "Encoding media")
				}
				if result := encodingTask.Save(w.deps.DB); result.Error != nil {
					w.encodingTaskLog("progress_persist_failed", task, time.Time{}, result.Error)
				}
			}
		}
	}()

	return sockFilePath
}

func (w *WorkerGroup) originalFileExists(fileID uint) (bool, error) {
	if res := w.deps.DB.First(&models.File{}, fileID); res.Error != nil {
		if errors.Is(res.Error, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, res.Error
	}
	return true, nil
}

func (w *WorkerGroup) addActiveEncoding(encoding ActiveEncoding) {
	w.activeEncodingsMu.Lock()
	w.activeEncodings = append(w.activeEncodings, encoding)
	w.activeEncodingsMu.Unlock()
}

func (w *WorkerGroup) cancelActiveEncodingsForFile(fileID uint, reason string) int {
	w.activeEncodingsMu.Lock()
	encodings := make([]ActiveEncoding, 0)
	for _, encoding := range w.activeEncodings {
		if encoding.Task.FileID == fileID {
			encodings = append(encodings, encoding)
		}
	}
	w.activeEncodingsMu.Unlock()

	for _, encoding := range encodings {
		w.encoderLogf(
			"task_cancel_requested",
			"task_type=%s file_id=%d file_uuid=%q task_id=%d task_name=%q storage_id=%q reason=%q",
			encoding.Task.Type,
			encoding.Task.FileID,
			encoding.Task.FileUUID,
			encoding.Task.ID,
			encoding.Task.Name,
			encoding.Task.StorageID,
			reason,
		)
		if encoding.Cancel != nil {
			encoding.Cancel()
		}
	}
	return len(encodings)
}

func (w *WorkerGroup) deleteActiveEncoding(task EncodingTask) {
	w.activeEncodingsMu.Lock()
	defer w.activeEncodingsMu.Unlock()

	foundIndex := -1
	for i, v := range w.activeEncodings {
		if v.Task.FileID == task.FileID && v.Task.ID == task.ID && v.Task.Type == task.Type {
			foundIndex = i
		}
	}
	if foundIndex < 0 {
		w.encoderLogf(
			"active_task_unregister_failed",
			"task_type=%s file_id=%d task_id=%d",
			task.Type,
			task.FileID,
			task.ID,
		)
		return
	}

	w.activeEncodings = removeFromArray(w.activeEncodings, foundIndex)
}

func (w *WorkerGroup) prepocessPgs(ctx context.Context, encodingTask models.Subtitle, absFolderOutput string, absFileInput *string) error {

	ffmpegOutputFile := fmt.Sprintf("%s.sup", encodingTask.OutputFile)
	ffmpegOutputFilePath := fmt.Sprintf("%s/%s", absFolderOutput, ffmpegOutputFile)
	pgsOutputFilePath := fmt.Sprintf("%s/%s.srt", absFolderOutput, encodingTask.OutputFile)
	defer os.Remove(ffmpegOutputFilePath)

	ffmpegCommand := "ffmpeg -y " +
		fmt.Sprintf("-i %s ", *absFileInput) + // input file
		"-an " + // disable audio
		"-vn " + // disable video stream
		fmt.Sprintf("-map 0:s:%d ", encodingTask.Index) + // mapping first audio stream
		fmt.Sprintf("-c:s copy ") + // setting audio codec
		ffmpegOutputFilePath // output file progress

	// convert to srt
	cmd := exec.CommandContext(ctx,
		"bash",
		"-c",
		ffmpegCommand)
	diagnostics := newEncodingDiagnosticTail(8192)
	cmd.Stdout = diagnostics
	cmd.Stderr = diagnostics
	if err := cmd.Run(); err != nil {
		return ffmpegEncodingError(ctx, err, diagnostics)
	}

	pgsFile, err := os.Open(ffmpegOutputFilePath)
	if err != nil {
		return fmt.Errorf("open extracted PGS subtitle: %w", err)
	}
	defer pgsFile.Close()

	requestCtx, cancel := context.WithTimeout(ctx, time.Minute*5)
	defer cancel()

	client := req.C()
	res, err := client.R().
		SetContext(requestCtx).
		SetFileReader("file", "subtitle.sup", pgsFile).
		Post(w.Config().PluginPgsServer)
	if err != nil {
		return fmt.Errorf("send PGS subtitle to preprocessing service: %w", err)
	}
	if !res.IsSuccessState() {
		return fmt.Errorf("PGS preprocessing service returned HTTP %d", res.StatusCode)
	}
	if err := os.WriteFile(pgsOutputFilePath, res.Bytes(), 0644); err != nil {
		return fmt.Errorf("save preprocessed PGS subtitle: %w", err)
	}
	*absFileInput = pgsOutputFilePath
	return nil
}

func extractNumber(input string) int {
	re := regexp.MustCompile(`[-]?\d[\d,]*[\.]?[\d{2}]*`)
	submatchall := re.FindAllString(input, -1)
	if len(submatchall) > 0 {
		if i, err := strconv.Atoi(submatchall[0]); err == nil {
			return i
		}
	}
	return 0
}

func dirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return err
	})
	return size, err
}

func removeFromArray[T any](s []T, i int) []T {
	if len(s) == 0 || len(s) <= i || i < 0 {
		return s
	}
	s[i] = s[len(s)-1]
	return s[:len(s)-1]
}
