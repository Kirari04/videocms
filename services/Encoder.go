package services

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
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
	ID      uint // qualityID | audioID | subID
	Channel *chan bool
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

var ActiveEncodings []ActiveEncoding
var limitChan chan bool

func Encoder() {
	limitChan = make(chan bool, config.ENV.MaxRunningEncodes)
	for {
		go loadEncodingTasks()
		time.Sleep(time.Second * 10)
	}
}

func ResetEncodingState() {
	if res := inits.DB.
		Model(&models.Quality{}).
		Where(&models.Quality{
			Encoding: true,
		}, "Encoding").
		Updates(map[string]interface{}{"encoding": false}); res.Error != nil {
		log.Println("Failed to reset encoding status on Quality", res.Error)
	}

	if res := inits.DB.
		Model(&models.Audio{}).
		Where(&models.Audio{
			Encoding: true,
		}, "Encoding").
		Updates(map[string]interface{}{"encoding": false}); res.Error != nil {
		log.Println("Failed to reset encoding status on Audio", res.Error)
	}

	if res := inits.DB.
		Model(&models.Subtitle{}).
		Where(&models.Subtitle{
			Encoding: true,
		}, "Encoding").
		Updates(map[string]interface{}{"encoding": false}); res.Error != nil {
		log.Println("Failed to reset encoding status on Subtitle", res.Error)
	}
}

func loadEncodingTasks() {
	var encodingTasks []EncodingTask

	// we want to encode the subtitles first, then audio and in the end the qualities
	// SUBTITLES
	var encodingSubs []models.Subtitle
	inits.DB.
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
		v.Save(inits.DB)
		encodingTasks = append(encodingTasks, EncodingTask{
			Type:   "sub",
			FileID: v.FileID,
			ID:     v.ID,
		})
	}

	// AUDIOS
	var encodingAudios []models.Audio
	if len(encodingSubs) < 10 {
		inits.DB.
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
		v.Save(inits.DB)
		encodingTasks = append(encodingTasks, EncodingTask{
			Type:   "audio",
			FileID: v.FileID,
			ID:     v.ID,
		})
	}

	// QUALITYS
	var encodingQualitys []models.Quality
	if len(encodingSubs) < 10 && len(encodingAudios) < 10 {
		inits.DB.
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
		v.Save(inits.DB)
		encodingTasks = append(encodingTasks, EncodingTask{
			Type:   "quality",
			FileID: v.FileID,
			ID:     v.ID,
		})
	}

	// RUNNING ENCODING TASKS
	for _, v := range encodingTasks {
		limitChan <- true
		go func(encodingTask EncodingTask) {
			defer func() {
				<-limitChan
			}()
			runEncode(encodingTask)
		}(v)
	}
}

func runEncode(encodingTaskInformation EncodingTask) {
	switch encodingTaskInformation.Type {
	case "quality":
		var encodingTask models.Quality
		inits.DB.Preload("File").Find(&encodingTask, encodingTaskInformation.ID)
		runEncodeQuality(encodingTask)
	case "audio":
		var encodingTask models.Audio
		inits.DB.Preload("File").Find(&encodingTask, encodingTaskInformation.ID)
		runEncodeAudio(encodingTask)
	case "sub":
		var encodingTask models.Subtitle
		inits.DB.Preload("File").Find(&encodingTask, encodingTaskInformation.ID)
		runEncodeSub(encodingTask)
	}
}

func runEncodeQuality(encodingTask models.Quality) {
	// we check if the original file has been deleted during the waittime
	if !originalFileExists(encodingTask.FileID) {
		encodingTask.Ready = false
		encodingTask.Encoding = false
		encodingTask.Failed = true
		encodingTask.Error = "Skipped because waiting for deletion"
		inits.DB.Save(&encodingTask)
		return
	}

	// log.Printf("Start encoding %s %s\n", encodingTask.File.UUID, encodingTask.Name)

	totalDuration := encodingTask.File.Duration
	os.MkdirAll(encodingTask.Path, 0777)

	var frameRateString string
	if encodingTask.AvgFrameRate > 0 {
		frameRateString = fmt.Sprintf("-r %.4f", encodingTask.AvgFrameRate)
	}

	absFileInput, _ := filepath.Abs(encodingTask.File.Path)
	absFolderOutput, _ := filepath.Abs(encodingTask.Path)
	encFilePath := fmt.Sprintf("%s/%s", absFolderOutput, encodingTask.OutputFile)

	var ffmpegCommand string = "echo Encoding type didnt match && exit 1"
	switch encodingTask.Type {
	case "hls":
		ffmpegAudio := "-an "
		if !encodingTask.Muted && encodingTask.AudioCodec != "" {
			ffmpegAudio = fmt.Sprintf("-c:a %s ", encodingTask.AudioCodec)
		}
		ffmpegCommand = "ffmpeg " +
			fmt.Sprintf("-i %s ", absFileInput) + // input file
			"-sn " + // disable subtitle
			ffmpegAudio +
			"-c:v libx264 " + // setting video codec libx264 | libaom-av1
			"-pix_fmt yuv420p " + // YUV 4:2:0
			"-profile:v high " + // force 8 bit
			fmt.Sprintf("-crf %d ", encodingTask.Crf) + // setting quality
			fmt.Sprintf("%s ", frameRateString) + // (optional) setting framerate
			fmt.Sprintf("-s %dx%d ", encodingTask.Width, encodingTask.Height) + // setting resolution
			"-f hls -hls_list_size 0 -hls_time 10 -start_number 0 " + // hls playlist
			fmt.Sprintf("%s ", encFilePath) + // output file
			fmt.Sprintf("-progress unix://%s -y", TempSock(
				totalDuration,
				fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
				&encodingTask,
			)) // progress tracking
	case "vp9":
		ffmpegAudio := "-an "
		if !encodingTask.Muted && encodingTask.AudioCodec != "" {
			ffmpegAudio = fmt.Sprintf("-c:a %s ", encodingTask.AudioCodec)
		}
		ffmpegCommand = "ffmpeg " + // starting pass 1
			fmt.Sprintf("-i %s ", absFileInput) + // input file
			"-c:v libvpx-vp9 " +
			"-pix_fmt yuv420p " + // YUV 4:2:0
			"-profile:v high " + // force 8 bit
			"-b:v 0 " +
			fmt.Sprintf("-crf %d ", encodingTask.Crf) + // setting quality
			fmt.Sprintf("%s ", frameRateString) + // (optional) setting framerate
			fmt.Sprintf("-s %dx%d ", encodingTask.Width, encodingTask.Height) + // setting resolution
			" -pass 1 -an -f null /dev/null && " + // pass 1 flags
			"ffmpeg " + // starting pass 2
			fmt.Sprintf("-i %s ", absFileInput) + // input file
			"-sn " + // disable subtitle
			ffmpegAudio +
			`-af aformat=channel_layouts="7.1|5.1|stereo" ` + // audio channel layouts
			"-c:v libvpx-vp9 " + // setting video codec libx264 | libaom-av1
			"-pass 2 " + // setting pass 2 flag
			fmt.Sprintf("-crf %d ", encodingTask.Crf) + // setting quality
			fmt.Sprintf("%s ", frameRateString) + // (optional) setting framerate
			fmt.Sprintf("-s %dx%d ", encodingTask.Width, encodingTask.Height) + // setting resolution
			fmt.Sprintf("%s ", encFilePath) + // output file
			fmt.Sprintf("-progress unix://%s -y", TempSock(
				totalDuration,
				fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
				&encodingTask,
			)) // progress tracking
	case "h264":
		ffmpegAudio := "-an "
		if !encodingTask.Muted && encodingTask.AudioCodec != "" {
			ffmpegAudio = fmt.Sprintf("-c:a %s ", encodingTask.AudioCodec)
		}
		ffmpegCommand =
			"ffmpeg " +
				fmt.Sprintf("-i %s ", absFileInput) + // input file
				"-sn " + // disable subtitle
				ffmpegAudio +
				`-af aformat=channel_layouts="7.1|5.1|stereo" ` + // audio channel layouts
				"-c:v libx264 " + // setting video codec libx264 | libaom-av1
				"-pix_fmt yuv420p " + // YUV 4:2:0
				"-profile:v high " + // force 8 bit
				fmt.Sprintf("-crf %d ", encodingTask.Crf) + // setting quality
				fmt.Sprintf("%s ", frameRateString) + // (optional) setting framerate
				fmt.Sprintf("-s %dx%d ", encodingTask.Width, encodingTask.Height) + // setting resolution
				fmt.Sprintf("%s ", encFilePath) + // output file
				fmt.Sprintf("-progress unix://%s -y", TempSock(
					totalDuration,
					fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
					&encodingTask,
				)) // progress tracking
	case "av1":
		ffmpegAudio := "-an "
		if !encodingTask.Muted && encodingTask.AudioCodec != "" {
			ffmpegAudio = fmt.Sprintf("-c:a %s ", encodingTask.AudioCodec)
		}
		ffmpegCommand = "ffmpeg " + // starting pass 1
			fmt.Sprintf("-i %s ", absFileInput) + // input file
			"-c:v libaom-av1 " +
			"-pix_fmt yuv420p " + // YUV 4:2:0
			"-profile:v high " + // force 8 bit
			fmt.Sprintf("-crf %d ", encodingTask.Crf) + // setting quality
			fmt.Sprintf("%s ", frameRateString) + // (optional) setting framerate
			fmt.Sprintf("-s %dx%d ", encodingTask.Width, encodingTask.Height) + // setting resolution
			" -pass 1 -an -f null /dev/null && " + // pass 1 flags
			"ffmpeg " + // starting pass 2
			fmt.Sprintf("-i %s ", absFileInput) + // input file
			"-sn " + // disable subtitle
			ffmpegAudio +
			`-af aformat=channel_layouts="7.1|5.1|stereo" ` + // audio channel layouts
			"-c:v libaom-av1 " + // setting video codec libx264 | libaom-av1
			"-pix_fmt yuv420p " + // YUV 4:2:0
			"-profile:v high " + // force 8 bit
			"-pass 2 " + // setting pass 2 flag
			fmt.Sprintf("-crf %d ", encodingTask.Crf) + // setting quality
			fmt.Sprintf("%s ", frameRateString) + // (optional) setting framerate
			fmt.Sprintf("-s %dx%d ", encodingTask.Width, encodingTask.Height) + // setting resolution
			fmt.Sprintf("%s ", encFilePath) + // output file
			fmt.Sprintf("-progress unix://%s -y", TempSock(
				totalDuration,
				fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
				&encodingTask,
			)) // progress tracking
	}

	cmd := exec.Command(
		"bash",
		"-c",
		ffmpegCommand)

	activeEncodingChannel := make(chan bool)
	defer deleteActiveEncoding(encodingTask.FileID, encodingTask.ID, "quality")

	ActiveEncodings = append(ActiveEncodings, ActiveEncoding{
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
			cmd.Process.Kill()
			log.Printf("killed encode (quality) of FileID %d QualityID %d\n", encodingTask.FileID, encodingTask.ID)
		}
	}()

	if err := cmd.Run(); err != nil {
		encodingTask.Ready = false
		encodingTask.Encoding = false
		encodingTask.Failed = true
		inits.DB.Save(&encodingTask)
		log.Printf("Error happend while encoding quality: %v", err.Error())
		log.Println(ffmpegCommand)
		return
	}

	qualitySize, err := helpers.DirSize(absFolderOutput)
	if err != nil {
		log.Printf("Failed to calc folder size after quality encode: %v", err)
	}

	encodingTask.Size = qualitySize
	encodingTask.Encoding = false
	encodingTask.Ready = true
	inits.DB.Save(&encodingTask)
}

func runEncodeAudio(encodingTask models.Audio) {
	// we check if the original file has been deleted during the waittime
	if !originalFileExists(encodingTask.FileID) {
		encodingTask.Ready = false
		encodingTask.Encoding = false
		encodingTask.Failed = true
		encodingTask.Error = "Skipped because waiting for deletion"
		inits.DB.Save(&encodingTask)
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
		ffmpegCommand = "ffmpeg " +
			fmt.Sprintf("-i %s ", absFileInput) + // input file
			"-sn " + // disable subtitle
			"-vn " + // disable video stream
			fmt.Sprintf("-map 0:a:%d ", encodingTask.Index) + // mapping first audio stream
			`-af aformat=channel_layouts="7.1|5.1|stereo" ` +
			fmt.Sprintf("-c:a %s ", encodingTask.Codec) + // setting audio codec
			"-f hls -hls_list_size 0 -hls_time 10 -start_number 0 " + // hls playlist
			fmt.Sprintf("%s/%s ", absFolderOutput, encodingTask.OutputFile) + // output file
			fmt.Sprintf("-progress unix://%s -y", TempSock(
				totalDuration,
				fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
				&encodingTask,
			)) // progress tracking
	case "ogg":
		ffmpegCommand = "ffmpeg " +
			fmt.Sprintf("-i %s ", absFileInput) + // input file
			"-sn " + // disable subtitle
			"-vn " + // disable video stream
			fmt.Sprintf("-map 0:a:%d ", encodingTask.Index) + // mapping first audio stream
			`-af aformat=channel_layouts="7.1|5.1|stereo" ` +
			fmt.Sprintf("-c:a %s ", encodingTask.Codec) + // setting audio codec
			fmt.Sprintf("%s/%s ", absFolderOutput, encodingTask.OutputFile) + // output file
			fmt.Sprintf("-progress unix://%s -y", TempSock(
				totalDuration,
				fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
				&encodingTask,
			)) // progress tracking
	case "mp3":
		ffmpegCommand = "ffmpeg " +
			fmt.Sprintf("-i %s ", absFileInput) + // input file
			"-sn " + // disable subtitle
			"-vn " + // disable video stream
			fmt.Sprintf("-map 0:a:%d ", encodingTask.Index) + // mapping first audio stream
			`-af aformat=channel_layouts="7.1|5.1|stereo" ` +
			fmt.Sprintf("-c:a %s ", encodingTask.Codec) + // setting audio codec
			fmt.Sprintf("%s/%s ", absFolderOutput, encodingTask.OutputFile) + // output file
			fmt.Sprintf("-progress unix://%s -y", TempSock(
				totalDuration,
				fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
				&encodingTask,
			)) // progress tracking
	}

	cmd := exec.Command(
		"bash",
		"-c",
		ffmpegCommand)

	activeEncodingChannel := make(chan bool)
	defer deleteActiveEncoding(encodingTask.FileID, encodingTask.ID, "audio")

	ActiveEncodings = append(ActiveEncodings, ActiveEncoding{
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
			cmd.Process.Kill()
			log.Printf("killed encode (quality) of FileID %d AudioID %d\n", encodingTask.FileID, encodingTask.ID)
		}
	}()

	if err := cmd.Run(); err != nil {
		encodingTask.Ready = false
		encodingTask.Encoding = false
		encodingTask.Failed = true
		inits.DB.Save(&encodingTask)
		log.Printf("Error happend while encoding audio: %v", err.Error())
		log.Println(ffmpegCommand)
		return
	}

	encodingTask.Encoding = false
	encodingTask.Ready = true
	inits.DB.Save(&encodingTask)
}

func runEncodeSub(encodingTask models.Subtitle) {
	// we check if the original file has been deleted during the waittime
	if !originalFileExists(encodingTask.FileID) {
		encodingTask.Ready = false
		encodingTask.Encoding = false
		encodingTask.Failed = true
		encodingTask.Error = "Skipped because waiting for deletion"
		inits.DB.Save(&encodingTask)
		return
	}

	// log.Printf("Start encoding %s %s\n", encodingTask.File.UUID, encodingTask.Name)

	totalDuration := encodingTask.File.Duration
	os.MkdirAll(encodingTask.Path, 0777)

	absFileInput, _ := filepath.Abs(encodingTask.File.Path)
	absFolderOutput, _ := filepath.Abs(encodingTask.Path)

	var ffmpegCommand string = "echo Subencoding type didnt match && exit 1"

	if encodingTask.OriginalCodec == "hdmv_pgs_subtitle" {
		// prepocess pgs
		if err := prepocessPgs(encodingTask, absFolderOutput, &absFileInput); err != nil {
			log.Printf("[Preprocess Error] %v", err)
			encodingTask.Ready = false
			encodingTask.Encoding = false
			encodingTask.Failed = true
			inits.DB.Save(&encodingTask)
			return
		}
		defer os.Remove(absFileInput) // delete srt file after encode

		switch encodingTask.Type {
		case "ass":
			ffmpegCommand = "ffmpeg " +
				fmt.Sprintf("-i %s ", absFileInput) + // input file
				fmt.Sprintf("-c:s %s ", encodingTask.Codec) + // setting audio codec
				fmt.Sprintf("%s/%s ", absFolderOutput, encodingTask.OutputFile) + // output file
				fmt.Sprintf("-progress unix://%s -y", TempSock(
					totalDuration,
					fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
					&encodingTask,
				)) // progress tracking
		case "vtt":
			ffmpegCommand = "ffmpeg " +
				fmt.Sprintf("-i %s ", absFileInput) + // input file
				fmt.Sprintf("-c:s %s ", encodingTask.Codec) + // setting audio codec
				fmt.Sprintf("%s/%s ", absFolderOutput, encodingTask.OutputFile) + // output file
				fmt.Sprintf("-progress unix://%s -y", TempSock(
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
				fmt.Sprintf("-progress unix://%s -y", TempSock(
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
				fmt.Sprintf("-progress unix://%s -y", TempSock(
					totalDuration,
					fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString()))),
					&encodingTask,
				)) // progress tracking
		}
	}

	cmd := exec.Command(
		"bash",
		"-c",
		ffmpegCommand)
	activeEncodingChannel := make(chan bool)
	defer deleteActiveEncoding(encodingTask.FileID, encodingTask.ID, "sub")

	ActiveEncodings = append(ActiveEncodings, ActiveEncoding{
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
			cmd.Process.Kill()
			log.Printf("killed encode (quality) of FileID %d SubID %d\n", encodingTask.FileID, encodingTask.ID)
		}
	}()

	if err := cmd.Run(); err != nil {
		encodingTask.Ready = false
		encodingTask.Encoding = false
		encodingTask.Failed = true
		inits.DB.Save(&encodingTask)
		log.Printf("Error happend while encoding subtitle: %v", err.Error())
		log.Println(ffmpegCommand)
		return
	}

	encodingTask.Encoding = false
	encodingTask.Ready = true
	inits.DB.Save(&encodingTask)
}

func TempSock(totalDuration float64, sockFileName string, encodingTask IwithProcess) string {
	sockFilePath := path.Join(os.TempDir(), sockFileName)
	l, err := net.Listen("unix", sockFilePath)
	if err != nil {
		panic(err)
	}

	go func() {
		re := regexp.MustCompile(`out_time_ms=(\d+)`)
		fd, err := l.Accept()
		if err != nil {
			log.Fatal("accept error:", err)
		}
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
				encodingTask.Save(inits.DB)
			}
		}
	}()

	return sockFilePath
}

func originalFileExists(fileId uint) bool {
	if res := inits.DB.First(&models.File{}, fileId); res.Error != nil {
		return false
	}
	return true
}

func deleteActiveEncoding(fileID uint, ID uint, Type string) {
	foundIndex := -1
	for i, v := range ActiveEncodings {
		if v.FileID == fileID && v.ID == ID && v.Type == Type {
			foundIndex = i
		}
	}
	if foundIndex < 0 {
		log.Printf("Failed to delete an deleteActiveEncoding with the fileID %v and ID %v", fileID, ID)
		return
	}

	ActiveEncodings = helpers.RemoveFromArray(ActiveEncodings, foundIndex)
}

func prepocessPgs(encodingTask models.Subtitle, absFolderOutput string, absFileInput *string) error {

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
	cmd := exec.Command(
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

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()

	client := req.C()
	res, err := client.R().
		SetContext(ctx).
		SetFileReader("file", "subtitle.sup", pgsFile).
		Post(config.ENV.PluginPgsServer)
	if err != nil {
		return fmt.Errorf("Error happend while scanning pgs subtitle: %v", err.Error())
	}
	if !res.IsSuccessState() {
		return fmt.Errorf("Error happend waiting for srt from pgs plugin: %v", err.Error())

	}
	if err := os.WriteFile(pgsOutputFilePath, res.Bytes(), 0644); err != nil {
		return fmt.Errorf("Error happend while saving srt from pgs: %v", err.Error())
	}
	*absFileInput = pgsOutputFilePath
	return nil
}
