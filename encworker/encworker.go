package encworker

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var runningEncodes int = 0
var maxRunningEncodes int = 2

func StartEncode() {
	maxRunningEncodes = int(config.ENV.MaxRunningEncodes)
	for {
		loadEncodingTasks()
		time.Sleep(time.Second * 10)
	}
}

func ConsoleEncode() {
	loadEncodingTasks()
}

func ResetEncodingState() {
	var encodingQualitys []models.Quality
	inits.DB.
		Model(&models.Quality{}).
		Preload("File").
		Where(&models.Quality{
			Encoding: true,
		}, "Encoding").
		Find(&encodingQualitys)

	for _, v := range encodingQualitys {
		v.Encoding = false
		inits.DB.Save(&v)
	}
}

func loadEncodingTasks() {
	var encodingQualitys []models.Quality
	inits.DB.
		Model(&models.Quality{}).
		Preload("File").
		Where(&models.Quality{
			Encoding: false,
			Ready:    false,
			Failed:   false,
		}, "Encoding", "Ready", "Failed").
		Find(&encodingQualitys)

	if len(encodingQualitys) > 0 {
		log.Printf("Loaded %v qualitys to encode", len(encodingQualitys))
	}

	for _, v := range encodingQualitys {
		v.Encoding = true
		inits.DB.Save(&v)

		go func(v models.Quality) {
			runEncode(v)
		}(v)

	}
}

func runEncode(encodingTask models.Quality) {
	for runningEncodes >= maxRunningEncodes {
		// we check if the original file has been deleted during the waittime
		if !originalFileExists(encodingTask.FileID) {
			encodingTask.Ready = false
			encodingTask.Encoding = false
			encodingTask.Failed = true
			encodingTask.Error = "Skipped because waiting for deletion"
			inits.DB.Save(&encodingTask)
			return
		}
		time.Sleep(time.Second * 1)
	}
	runningEncodes += 1

	// we check if the original file has been deleted during the waittime
	if !originalFileExists(encodingTask.FileID) {
		runningEncodes -= 1
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
		ffmpegCommand = "ffmpeg " +
			fmt.Sprintf("-i %s ", absFileInput) + // input file
			"-sn " + // disable subtitle
			"-an " + // disable audio
			"-map 0:v:0 " + // mapping first video stream
			"-c:v libx264 " + // setting video codec libx264 | libaom-av1
			fmt.Sprintf("-crf %d ", encodingTask.Crf) + // setting quality
			fmt.Sprintf("%s ", frameRateString) + // (optional) setting framerate
			fmt.Sprintf("-s %dx%d ", encodingTask.Width, encodingTask.Height) + // setting resolution
			"-f hls -hls_list_size 0 -hls_time 10 -start_number 0 " + // hls playlist
			fmt.Sprintf("%s ", encFilePath) + // output file
			fmt.Sprintf("-progress unix://%s -y", TempSock(totalDuration, &encodingTask)) // progress tracking
	case "vp9":

		ffmpegCommand = "ffmpeg " + // starting pass 1
			fmt.Sprintf("-i %s ", absFileInput) + // input file
			"-c:v libvpx-vp9 " +
			"-b:v 0 " +
			fmt.Sprintf("-crf %d ", encodingTask.Crf) + // setting quality
			fmt.Sprintf("%s ", frameRateString) + // (optional) setting framerate
			fmt.Sprintf("-s %dx%d ", encodingTask.Width, encodingTask.Height) + // setting resolution
			" -pass 1 -an -f null /dev/null && " + // pass 1 flags
			"ffmpeg " + // starting pass 2
			fmt.Sprintf("-i %s ", absFileInput) + // input file
			"-sn " + // disable subtitle
			// "-an " + // disable audio
			`-af aformat=channel_layouts="7.1|5.1|stereo" ` + // audio channel layouts
			"-c:v libvpx-vp9 " + // setting video codec libx264 | libaom-av1
			"-c:a libopus " + // setting audio codec
			"-pass 2 " + // setting pass 2 flag
			fmt.Sprintf("-crf %d ", encodingTask.Crf) + // setting quality
			fmt.Sprintf("%s ", frameRateString) + // (optional) setting framerate
			fmt.Sprintf("-s %dx%d ", encodingTask.Width, encodingTask.Height) + // setting resolution
			fmt.Sprintf("%s ", encFilePath) + // output file
			fmt.Sprintf("-progress unix://%s -y", TempSock(totalDuration, &encodingTask)) // progress tracking
	case "av1":
		ffmpegCommand = "ffmpeg " + // starting pass 1
			fmt.Sprintf("-i %s ", absFileInput) + // input file
			"-c:v libaom-av1 " +
			fmt.Sprintf("-crf %d ", encodingTask.Crf) + // setting quality
			fmt.Sprintf("%s ", frameRateString) + // (optional) setting framerate
			fmt.Sprintf("-s %dx%d ", encodingTask.Width, encodingTask.Height) + // setting resolution
			" -pass 1 -an -f null /dev/null && " + // pass 1 flags
			"ffmpeg " + // starting pass 2
			fmt.Sprintf("-i %s ", absFileInput) + // input file
			"-sn " + // disable subtitle
			// "-an " + // disable audio
			`-af aformat=channel_layouts="7.1|5.1|stereo" ` + // audio channel layouts
			"-c:v libaom-av1 " + // setting video codec libx264 | libaom-av1
			"-c:a aac " + // setting audio codec
			"-pass 2 " + // setting pass 2 flag
			fmt.Sprintf("-crf %d ", encodingTask.Crf) + // setting quality
			fmt.Sprintf("%s ", frameRateString) + // (optional) setting framerate
			fmt.Sprintf("-s %dx%d ", encodingTask.Width, encodingTask.Height) + // setting resolution
			fmt.Sprintf("%s ", encFilePath) + // output file
			fmt.Sprintf("-progress unix://%s -y", TempSock(totalDuration, &encodingTask)) // progress tracking
	}

	cmd := exec.Command(
		"bash",
		"-c",
		ffmpegCommand)

	if err := cmd.Run(); err != nil {
		runningEncodes -= 1
		encodingTask.Ready = false
		encodingTask.Encoding = false
		encodingTask.Failed = true
		inits.DB.Save(&encodingTask)
		log.Printf("Error happend while encoding: %v", err.Error())
		log.Println(ffmpegCommand)
		return
	}

	encodingTask.Encoding = false
	encodingTask.Ready = true
	inits.DB.Save(&encodingTask)
	// log.Printf("Finish encoding %s %s\n", encodingTask.File.UUID, encodingTask.Name)
	runningEncodes -= 1
}

func TempSock(totalDuration float64, encodingTask *models.Quality) string {
	s1 := rand.NewSource(time.Now().UnixNano())
	r1 := rand.New(s1)

	sockFileName := path.Join(os.TempDir(), fmt.Sprintf("%s_%vx%v_%d_sock", encodingTask.File.UUID, encodingTask.Width, encodingTask.Height, r1.Intn(10000)))
	l, err := net.Listen("unix", sockFileName)
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

					encodingTask.Progress = floatProg
				}
				inits.DB.Save(encodingTask)
			}
		}
	}()

	return sockFileName
}

func originalFileExists(fileId uint) bool {
	if res := inits.DB.First(&models.File{}, fileId); res.Error != nil {
		return false
	}
	return true
}
