package services

import (
	"ch/kirari04/videocms/models"
	"context"
	"crypto/sha256"
	"fmt"
	"log"
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
	Type    string
	FileID  uint
	ID      uint       // qualityID | audioID | subID
	Channel *chan bool // sending true will kill the encoding process
}

type EncodingTask struct {
	Type   string
	FileID uint
	ID     uint // qualityID | audioID | subID
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
	w.limitChan = make(chan bool, w.Config().MaxRunningEncodes)
	for {
		go w.loadEncodingTasks(ctx)
		if !sleepContext(ctx, time.Second*10) {
			return
		}
	}
}

func (w *WorkerGroup) ResetEncodingState() {
	if res := w.deps.DB.
		Model(&models.Quality{}).
		Where(&models.Quality{
			Encoding: true,
		}, "Encoding").
		Or("progress > ?", 0).
		Updates(map[string]interface{}{"encoding": false, "progress": 0}); res.Error != nil {
		log.Println("Failed to reset encoding status on Quality", res.Error)
	}

	if res := w.deps.DB.
		Model(&models.Audio{}).
		Where(&models.Audio{
			Encoding: true,
		}, "Encoding").
		Or("progress > ?", 0).
		Updates(map[string]interface{}{"encoding": false, "progress": 0}); res.Error != nil {
		log.Println("Failed to reset encoding status on Audio", res.Error)
	}

	if res := w.deps.DB.
		Model(&models.Subtitle{}).
		Where(&models.Subtitle{
			Encoding: true,
		}, "Encoding").
		Or("progress > ?", 0).
		Updates(map[string]interface{}{"encoding": false, "progress": 0}); res.Error != nil {
		log.Println("Failed to reset encoding status on Subtitle", res.Error)
	}
}

func (w *WorkerGroup) loadEncodingTasks(ctx context.Context) {
	var encodingTasks []EncodingTask

	// we want to encode the subtitles first, then audio and in the end the qualities
	// SUBTITLES
	var encodingSubs []models.Subtitle
	w.deps.DB.
		Model(&models.Subtitle{}).
		Preload("File").
		Where(&models.Subtitle{
			Encoding: false,
			Ready:    false,
			Failed:   false,
		}, "Encoding", "Ready", "Failed").
		Order("id ASC").
		Limit(10).
		Find(&encodingSubs)

	if len(encodingSubs) > 0 {
		log.Printf("Loaded %v subs to encode", len(encodingSubs))
	}

	for _, v := range encodingSubs {
		v.Encoding = true
		v.Save(w.deps.DB)
		encodingTasks = append(encodingTasks, EncodingTask{
			Type:   "sub",
			FileID: v.FileID,
			ID:     v.ID,
		})
	}

	// AUDIOS
	var encodingAudios []models.Audio
	if len(encodingSubs) < 10 {
		w.deps.DB.
			Model(&models.Audio{}).
			Preload("File").
			Where(&models.Audio{
				Encoding: false,
				Ready:    false,
				Failed:   false,
			}, "Encoding", "Ready", "Failed").
			Order("id ASC").
			Limit(10).
			Find(&encodingAudios)
	}

	if len(encodingAudios) > 0 {
		log.Printf("Loaded %v audios to encode", len(encodingAudios))
	}

	for _, v := range encodingAudios {
		v.Encoding = true
		v.Save(w.deps.DB)
		encodingTasks = append(encodingTasks, EncodingTask{
			Type:   "audio",
			FileID: v.FileID,
			ID:     v.ID,
		})
	}

	// QUALITYS
	var encodingQualitys []models.Quality
	if len(encodingSubs) < 10 && len(encodingAudios) < 10 {
		w.deps.DB.
			Model(&models.Quality{}).
			Preload("File").
			Where(&models.Quality{
				Encoding: false,
				Ready:    false,
				Failed:   false,
			}, "Encoding", "Ready", "Failed").
			Order("id ASC").
			Limit(10).
			Find(&encodingQualitys)
	}

	if len(encodingQualitys) > 0 {
		log.Printf("Loaded %v qualitys to encode", len(encodingQualitys))
	}

	for _, v := range encodingQualitys {
		v.Encoding = true
		v.Save(w.deps.DB)
		encodingTasks = append(encodingTasks, EncodingTask{
			Type:   "quality",
			FileID: v.FileID,
			ID:     v.ID,
		})
	}

	// RUNNING ENCODING TASKS
	for _, v := range encodingTasks {
		select {
		case <-ctx.Done():
			return
		case w.limitChan <- true:
		}
		go func(encodingTask EncodingTask) {
			defer func() {
				<-w.limitChan
			}()
			w.runEncode(ctx, encodingTask)
		}(v)
	}
}

func (w *WorkerGroup) runEncode(ctx context.Context, encodingTaskInformation EncodingTask) {
	switch encodingTaskInformation.Type {
	case "quality":
		var encodingTask models.Quality
		w.deps.DB.Preload("File").Find(&encodingTask, encodingTaskInformation.ID)
		w.runEncodeQuality(ctx, encodingTask)
	case "audio":
		var encodingTask models.Audio
		w.deps.DB.Preload("File").Find(&encodingTask, encodingTaskInformation.ID)
		w.runEncodeAudio(ctx, encodingTask)
	case "sub":
		var encodingTask models.Subtitle
		w.deps.DB.Preload("File").Find(&encodingTask, encodingTaskInformation.ID)
		w.runEncodeSub(ctx, encodingTask)
	}
}

func (w *WorkerGroup) runEncodeQuality(ctx context.Context, encodingTask models.Quality) {
	// we check if the original file has been deleted during the waittime
	if !w.originalFileExists(encodingTask.FileID) {
		encodingTask.Ready = false
		encodingTask.Encoding = false
		encodingTask.Failed = true
		encodingTask.Error = "Skipped because waiting for deletion"
		w.deps.DB.Save(&encodingTask)
		return
	}

	// log.Printf("Start encoding %s %s\n", encodingTask.File.UUID, encodingTask.Name)

	totalDuration := encodingTask.File.Duration
	os.MkdirAll(encodingTask.Path, 0777)

	var frameRateString string
	var segmenDuration int = 4
	if encodingTask.AvgFrameRate > 0 {
		frameRateString = fmt.Sprintf("-r %.4f", encodingTask.AvgFrameRate)
	}

	absFileInput, _ := filepath.Abs(encodingTask.File.Path)
	absFolderOutput, _ := filepath.Abs(encodingTask.Path)
	encFilePath := fmt.Sprintf("%s/%s", absFolderOutput, encodingTask.OutputFile)

	var ffmpegCommand string = "echo Encoding type didnt match && exit 1"
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
			fmt.Sprintf("-progress unix://%s -y", w.tempSock(
				totalDuration,
				fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
				&encodingTask,
			)) // progress tracking
	}

	cmd := exec.CommandContext(ctx,
		"bash",
		"-c",
		ffmpegCommand)

	activeEncodingChannel := make(chan bool)
	defer w.deleteActiveEncoding(encodingTask.FileID, encodingTask.ID, "quality")

	w.addActiveEncoding(ActiveEncoding{
		Type:    "quality",
		FileID:  encodingTask.FileID,
		ID:      encodingTask.ID,
		Channel: &activeEncodingChannel,
	})
	go func() {
		for {
			_, ok := <-activeEncodingChannel
			if !ok {
				break
			}
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			log.Printf("killed encode (quality) of FileID %d QualityID %d\n", encodingTask.FileID, encodingTask.ID)
		}
	}()

	start := time.Now()
	if err := cmd.Run(); err != nil {
		encodingTask.Ready = false
		encodingTask.Encoding = false
		encodingTask.Failed = true
		w.deps.DB.Save(&encodingTask)
		log.Printf("Error happend while encoding quality: %v", err.Error())
		log.Println(ffmpegCommand)
		return
	}
	duration := time.Since(start).Seconds()
	w.logic.TrackEncoding(encodingTask.File.UserID, encodingTask.FileID, "quality", duration)

	qualitySize, err := dirSize(absFolderOutput)
	if err != nil {
		log.Printf("Failed to calc folder size after quality encode: %v", err)
	}

	encodingTask.Size = qualitySize
	encodingTask.Encoding = false
	encodingTask.Ready = true
	w.deps.DB.Save(&encodingTask)
}

func (w *WorkerGroup) runEncodeAudio(ctx context.Context, encodingTask models.Audio) {
	// we check if the original file has been deleted during the waittime
	if !w.originalFileExists(encodingTask.FileID) {
		encodingTask.Ready = false
		encodingTask.Encoding = false
		encodingTask.Failed = true
		encodingTask.Error = "Skipped because waiting for deletion"
		w.deps.DB.Save(&encodingTask)
		return
	}

	// log.Printf("Start encoding %s %s\n", encodingTask.File.UUID, encodingTask.Name)

	totalDuration := encodingTask.File.Duration
	os.MkdirAll(encodingTask.Path, 0777)

	absFileInput, _ := filepath.Abs(encodingTask.File.Path)
	absFolderOutput, _ := filepath.Abs(encodingTask.Path)

	var ffmpegCommand string = "echo Audioencoding type didnt match && exit 1"
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
			fmt.Sprintf("-progress unix://%s -y", w.tempSock(
				totalDuration,
				fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
				&encodingTask,
			)) // progress tracking
	}

	cmd := exec.CommandContext(ctx,
		"bash",
		"-c",
		ffmpegCommand)

	activeEncodingChannel := make(chan bool)
	defer w.deleteActiveEncoding(encodingTask.FileID, encodingTask.ID, "audio")

	w.addActiveEncoding(ActiveEncoding{
		Type:    "audio",
		FileID:  encodingTask.FileID,
		ID:      encodingTask.ID,
		Channel: &activeEncodingChannel,
	})
	go func() {
		for {
			_, ok := <-activeEncodingChannel
			if !ok {
				break
			}
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			log.Printf("killed encode (quality) of FileID %d AudioID %d\n", encodingTask.FileID, encodingTask.ID)
		}
	}()

	start := time.Now()
	if err := cmd.Run(); err != nil {
		encodingTask.Ready = false
		encodingTask.Encoding = false
		encodingTask.Failed = true
		w.deps.DB.Save(&encodingTask)
		log.Printf("Error happend while encoding audio: %v", err.Error())
		log.Println(ffmpegCommand)
		return
	}
	duration := time.Since(start).Seconds()
	w.logic.TrackEncoding(encodingTask.File.UserID, encodingTask.FileID, "audio", duration)

	encodingTask.Encoding = false
	encodingTask.Ready = true
	w.deps.DB.Save(&encodingTask)
}

func (w *WorkerGroup) runEncodeSub(ctx context.Context, encodingTask models.Subtitle) {
	// we check if the original file has been deleted during the waittime
	if !w.originalFileExists(encodingTask.FileID) {
		encodingTask.Ready = false
		encodingTask.Encoding = false
		encodingTask.Failed = true
		encodingTask.Error = "Skipped because waiting for deletion"
		w.deps.DB.Save(&encodingTask)
		return
	}

	// log.Printf("Start encoding %s %s\n", encodingTask.File.UUID, encodingTask.Name)

	totalDuration := encodingTask.File.Duration
	os.MkdirAll(encodingTask.Path, 0777)

	absFileInput, _ := filepath.Abs(encodingTask.File.Path)
	absFolderOutput, _ := filepath.Abs(encodingTask.Path)

	var ffmpegCommand string = "echo Subencoding type didnt match && exit 1"

	if encodingTask.OriginalCodec == "hdmv_pgs_subtitle" {
		cfg := w.Config()
		if cfg.EnablePluginPgsServer == nil || *cfg.EnablePluginPgsServer == false {
			log.Printf("PluginPgsServer disabled")
			encodingTask.Ready = false
			encodingTask.Encoding = false
			encodingTask.Failed = true
			w.deps.DB.Save(&encodingTask)
			return
		}

		// prepocess pgs
		if err := w.prepocessPgs(ctx, encodingTask, absFolderOutput, &absFileInput); err != nil {
			log.Printf("[Preprocess Error] %v", err)
			encodingTask.Ready = false
			encodingTask.Encoding = false
			encodingTask.Failed = true
			w.deps.DB.Save(&encodingTask)
			return
		}
		defer os.Remove(absFileInput) // delete srt file after encode

		switch encodingTask.Type {
		case "ass":
			ffmpegCommand = "ffmpeg " +
				fmt.Sprintf("-i %s ", absFileInput) + // input file
				fmt.Sprintf("-c:s %s ", encodingTask.Codec) + // setting audio codec
				fmt.Sprintf("%s/%s ", absFolderOutput, encodingTask.OutputFile) + // output file
				fmt.Sprintf("-progress unix://%s -y", w.tempSock(
					totalDuration,
					fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
					&encodingTask,
				)) // progress tracking
		case "vtt":
			ffmpegCommand = "ffmpeg " +
				fmt.Sprintf("-i %s ", absFileInput) + // input file
				fmt.Sprintf("-c:s %s ", encodingTask.Codec) + // setting audio codec
				fmt.Sprintf("%s/%s ", absFolderOutput, encodingTask.OutputFile) + // output file
				fmt.Sprintf("-progress unix://%s -y", w.tempSock(
					totalDuration,
					fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
					&encodingTask,
				)) // progress tracking
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
				fmt.Sprintf("-progress unix://%s -y", w.tempSock(
					totalDuration,
					fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
					&encodingTask,
				)) // progress tracking
		case "vtt":
			ffmpegCommand = "ffmpeg " +
				fmt.Sprintf("-i %s ", absFileInput) + // input file
				"-an " + // disable audio
				"-vn " + // disable video stream
				fmt.Sprintf("-map 0:s:%d ", encodingTask.Index) + // mapping first audio stream
				fmt.Sprintf("-c:s %s ", encodingTask.Codec) + // setting audio codec
				fmt.Sprintf("%s/%s ", absFolderOutput, encodingTask.OutputFile) + // output file
				fmt.Sprintf("-progress unix://%s -y", w.tempSock(
					totalDuration,
					fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
					&encodingTask,
				)) // progress tracking
		}
	}

	cmd := exec.CommandContext(ctx,
		"bash",
		"-c",
		ffmpegCommand)
	activeEncodingChannel := make(chan bool)
	defer w.deleteActiveEncoding(encodingTask.FileID, encodingTask.ID, "sub")

	w.addActiveEncoding(ActiveEncoding{
		Type:    "sub",
		FileID:  encodingTask.FileID,
		ID:      encodingTask.ID,
		Channel: &activeEncodingChannel,
	})
	go func() {
		for {
			_, ok := <-activeEncodingChannel
			if !ok {
				break
			}
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			log.Printf("killed encode (quality) of FileID %d SubID %d\n", encodingTask.FileID, encodingTask.ID)
		}
	}()

	start := time.Now()
	if err := cmd.Run(); err != nil {
		encodingTask.Ready = false
		encodingTask.Encoding = false
		encodingTask.Failed = true
		w.deps.DB.Save(&encodingTask)
		log.Printf("Error happend while encoding subtitle: %v", err.Error())
		log.Println(ffmpegCommand)
		return
	}
	duration := time.Since(start).Seconds()
	w.logic.TrackEncoding(encodingTask.File.UserID, encodingTask.FileID, "sub", duration)

	encodingTask.Encoding = false
	encodingTask.Ready = true
	w.deps.DB.Save(&encodingTask)
}

func (w *WorkerGroup) tempSock(totalDuration float64, sockFileName string, encodingTask IwithProcess) string {
	sockFilePath := path.Join(os.TempDir(), sockFileName)
	_ = os.Remove(sockFilePath)
	l, err := net.Listen("unix", sockFilePath)
	if err != nil {
		log.Printf("failed to create encoder progress socket %s: %v", sockFilePath, err)
		return sockFilePath
	}

	go func() {
		defer l.Close()
		re := regexp.MustCompile(`out_time_ms=(\d+)`)
		fd, err := l.Accept()
		if err != nil {
			log.Printf("encoder progress socket accept error: %v", err)
			return
		}
		defer fd.Close()
		buf := make([]byte, 16)
		data := ""
		progress := ""
		for {
			_, err := fd.Read(buf)
			if err != nil {
				return
			}
			data += string(buf)
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
					fmt.Println("could not save progress in database")
				}
				if floatProg != 0 {
					encodingTask.SetProcess(floatProg)
				}
				encodingTask.Save(w.deps.DB)
			}
		}
	}()

	return sockFilePath
}

func (w *WorkerGroup) originalFileExists(fileId uint) bool {
	if res := w.deps.DB.First(&models.File{}, fileId); res.Error != nil {
		return false
	}
	return true
}

func (w *WorkerGroup) addActiveEncoding(encoding ActiveEncoding) {
	w.activeEncodingsMu.Lock()
	w.activeEncodings = append(w.activeEncodings, encoding)
	w.activeEncodingsMu.Unlock()
}

func (w *WorkerGroup) activeEncodingsForFile(fileID uint) []ActiveEncoding {
	w.activeEncodingsMu.Lock()
	defer w.activeEncodingsMu.Unlock()

	encodings := make([]ActiveEncoding, 0)
	for _, encoding := range w.activeEncodings {
		if encoding.FileID == fileID {
			encodings = append(encodings, encoding)
		}
	}
	return encodings
}

func (w *WorkerGroup) deleteActiveEncoding(fileID uint, ID uint, Type string) {
	w.activeEncodingsMu.Lock()
	defer w.activeEncodingsMu.Unlock()

	foundIndex := -1
	for i, v := range w.activeEncodings {
		if v.FileID == fileID && v.ID == ID && v.Type == Type {
			foundIndex = i
		}
	}
	if foundIndex < 0 {
		log.Printf("Failed to delete an deleteActiveEncoding with the fileID %v and ID %v", fileID, ID)
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
	if err := cmd.Run(); err != nil {
		log.Println(ffmpegCommand)
		return fmt.Errorf("Error happend while encoding subtitle: %v", err.Error())
	}

	pgsFile, err := os.Open(ffmpegOutputFilePath)
	if err != nil {
		return fmt.Errorf("Error happend while opening pgs subtitle: %v", err.Error())
	}

	requestCtx, cancel := context.WithTimeout(ctx, time.Minute*5)
	defer cancel()

	client := req.C()
	res, err := client.R().
		SetContext(requestCtx).
		SetFileReader("file", "subtitle.sup", pgsFile).
		Post(w.Config().PluginPgsServer)
	if err != nil {
		return fmt.Errorf("Error happend while scanning pgs subtitle: %v", err.Error())
	}
	if !res.IsSuccessState() {
		return fmt.Errorf("Error happend waiting for srt from pgs plugin: %v", err)

	}
	if err := os.WriteFile(pgsOutputFilePath, res.Bytes(), 0644); err != nil {
		return fmt.Errorf("Error happend while saving srt from pgs: %v", err.Error())
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
