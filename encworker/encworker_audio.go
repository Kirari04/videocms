package encworker

import (
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

var runningEncodes_audio int = 0
var maxRunningEncodes_audio int = 1

func StartEncode_audio() {
	for {
		loadEncodingTasks_audio()
		time.Sleep(time.Second * 10)
	}
}

func ResetEncodingState_audio() {
	var encodingAudios []models.Audio
	inits.DB.
		Model(&models.Audio{}).
		Preload("File").
		Where(&models.Audio{
			Encoding: true,
		}, "Encoding").
		Find(&encodingAudios)

	for _, v := range encodingAudios {
		v.Encoding = false
		inits.DB.Save(&v)
	}
}

func loadEncodingTasks_audio() {
	var encodingAudios []models.Audio
	inits.DB.
		Model(&models.Audio{}).
		Preload("File").
		Where(&models.Audio{
			Encoding: false,
			Ready:    false,
			Failed:   false,
		}, "Encoding", "Ready", "Failed").
		Find(&encodingAudios)

	if len(encodingAudios) > 0 {
		log.Printf("Loaded %v audios to encode", len(encodingAudios))
	}

	for _, v := range encodingAudios {
		v.Encoding = true
		inits.DB.Save(&v)

		go func(v models.Audio) {
			runEncode_audio(v)
		}(v)

	}
}

func runEncode_audio(encodingTask models.Audio) {
	for runningEncodes_audio >= maxRunningEncodes_audio {
		time.Sleep(time.Second * 10)
	}
	runningEncodes_audio += 1
	log.Printf("Start encoding %s %s\n", encodingTask.File.UUID, encodingTask.Name)

	totalDuration := encodingTask.File.Duration
	os.MkdirAll(encodingTask.Path, 0777)

	absFileInput, _ := filepath.Abs(encodingTask.File.Path)
	absFolderOutput, _ := filepath.Abs(encodingTask.Path)
	encFilePath := fmt.Sprintf("%s/audio.m3u8", absFolderOutput)

	ffmpegCommand := "ffmpeg " +
		fmt.Sprintf("-i %s ", absFileInput) + // input file
		"-sn " + // disable subtitle
		"-vn " + // disable video stream
		"-map 0 " + // mapping first audio stream
		`-af aformat=channel_layouts="7.1|5.1|stereo" ` +
		"-c:a aac " + // disable audio
		"-f hls -hls_list_size 0 -hls_time 10 -start_number 0 " + // hls playlist
		fmt.Sprintf("%s ", encFilePath) + // output file
		fmt.Sprintf("-progress unix://%s -y", TempSock_audio(totalDuration, &encodingTask)) // progress tracking
	log.Println(ffmpegCommand)
	cmd := exec.Command(
		"bash",
		"-c",
		ffmpegCommand)

	if err := cmd.Run(); err != nil {
		runningEncodes_audio -= 1
		encodingTask.Ready = false
		encodingTask.Encoding = false
		encodingTask.Failed = true
		inits.DB.Save(&encodingTask)
		log.Printf("Error happend while encoding: %v", err.Error())
		return
	}

	encodingTask.Encoding = false
	encodingTask.Ready = true
	inits.DB.Save(&encodingTask)
	log.Printf("Finish encoding %s %s\n", encodingTask.File.UUID, encodingTask.Name)
	runningEncodes_audio -= 1
}

func TempSock_audio(totalDuration float64, encodingTask *models.Audio) string {
	rand.Seed(time.Now().Unix())
	sockFileName := path.Join(os.TempDir(), fmt.Sprintf("%s_%d_sock", encodingTask.UUID, rand.Int()))
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
