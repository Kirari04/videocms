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
	"regexp"
	"strconv"
	"strings"
	"time"
)

var runningEncodes_sub int = 0
var maxrunningEncodes_sub int = 2

func StartEncode_sub() {
	for {
		loadEncodingTasks_sub()
		time.Sleep(time.Second * 10)
	}
}

func ConsoleEncode_sub() {
	loadEncodingTasks_sub()
}

func ResetEncodingState_sub() {
	var encodingSubtitles []models.Subtitle
	inits.DB.
		Model(&models.Subtitle{}).
		Preload("File").
		Where(&models.Subtitle{
			Encoding: true,
		}, "Encoding").
		Find(&encodingSubtitles)

	for _, v := range encodingSubtitles {
		v.Encoding = false
		inits.DB.Save(&v)
	}
}

func loadEncodingTasks_sub() {
	var encodingSubtitles []models.Subtitle
	inits.DB.
		Model(&models.Subtitle{}).
		Preload("File").
		Where(&models.Subtitle{
			Encoding: false,
			Ready:    false,
			Failed:   false,
		}, "Encoding", "Ready", "Failed").
		Find(&encodingSubtitles)

	if len(encodingSubtitles) > 0 {
		log.Printf("Loaded %v Subtitles to encode", len(encodingSubtitles))
	}

	for _, v := range encodingSubtitles {
		v.Encoding = true
		inits.DB.Save(&v)

		go func(v models.Subtitle) {
			runEncode_sub(v)
		}(v)

	}
}

func runEncode_sub(encodingTask models.Subtitle) {
	for runningEncodes_sub >= maxrunningEncodes_sub {
		time.Sleep(time.Second * 10)
	}
	runningEncodes_sub += 1
	log.Printf("Start encoding %s %s\n", encodingTask.File.UUID, encodingTask.Name)

	totalDuration := encodingTask.File.Duration
	os.MkdirAll(encodingTask.Path, 0777)

	ffmpegCommand := "ffmpeg " +
		fmt.Sprintf("-i %s ", encodingTask.File.Path) + // input file
		"-an " + // disable audio
		"-vn " + // disable video stream
		fmt.Sprintf("-map 0:s:%d ", encodingTask.Index) + // mapping first audio stream
		fmt.Sprintf("%s/out.ass ", encodingTask.Path) + // output file
		fmt.Sprintf("-progress unix://%s -y", TempSock_sub(totalDuration, &encodingTask)) // progress tracking

	cmd := exec.Command(
		"bash",
		"-c",
		ffmpegCommand)

	if err := cmd.Run(); err != nil {
		runningEncodes_sub -= 1
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
	log.Printf("Finish encoding %s %s\n", encodingTask.File.UUID, encodingTask.Name)
	runningEncodes_sub -= 1
}

func TempSock_sub(totalDuration float64, encodingTask *models.Subtitle) string {
	s1 := rand.NewSource(time.Now().UnixNano())
	r1 := rand.New(s1)

	sockFileName := path.Join(os.TempDir(), fmt.Sprintf("%s_subtitle_%d_sock", encodingTask.UUID, r1.Intn(10000)))
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
