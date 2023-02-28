package encworker

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	ffmpeg_go "github.com/u2takey/ffmpeg-go"
)

var runningEncodes int = 0
var maxRunningEncodes int = 2

func StartEncode() {
	for true {
		loadEncodingTasks()
		time.Sleep(time.Second * 10)
	}
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
		}, "Encoding", "Ready", "Error").
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
		time.Sleep(time.Second * 10)
	}
	runningEncodes += 1
	log.Printf("Start encoding %s %s\n", encodingTask.File.Name, encodingTask.Name)

	totalDuration := encodingTask.File.Duration
	encFilePath := fmt.Sprintf("%s/%s", encodingTask.Path, encodingTask.OutputFile)
	os.MkdirAll(encodingTask.Path, 0777)

	err := ffmpeg_go.Input(encodingTask.File.Path).
		Output(encFilePath, ffmpeg_go.KwArgs{
			"c:v":           "libx264",
			"c:a":           "aac",
			"preset":        "fast",
			"s":             fmt.Sprintf("%dx%d", encodingTask.Width, encodingTask.Height),
			"crf":           encodingTask.Crf,
			"start_number":  0,
			"hls_time":      10,
			"hls_list_size": 0,
			"f":             "hls",
		}).
		GlobalArgs("-progress", "unix://"+TempSock(totalDuration, &encodingTask)).
		OverWriteOutput().
		Run()
	if err != nil {
		runningEncodes -= 1
		encodingTask.Ready = false
		encodingTask.Failed = true
		inits.DB.Save(&encodingTask)
		log.Printf("Error happend while encoding: %v", err.Error())
		return
	}

	encodingTask.Ready = true
	inits.DB.Save(&encodingTask)
	log.Printf("Finish encoding %s %s\n", encodingTask.File.Name, encodingTask.Name)
	runningEncodes -= 1
}

func TempSock(totalDuration float64, encodingTask *models.Quality) string {
	rand.Seed(time.Now().Unix())
	sockFileName := path.Join(os.TempDir(), fmt.Sprintf("%s_%vx%v_%d_sock", encodingTask.File.UUID, encodingTask.Width, encodingTask.Height, rand.Int()))
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

					*&encodingTask.Progress = floatProg
				}
				inits.DB.Save(encodingTask)
			}
		}
	}()

	return sockFileName
}
