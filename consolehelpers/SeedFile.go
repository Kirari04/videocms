package consolehelpers

import (
	"ch/kirari04/videocms/encworker"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.in/vansante/go-ffprobe.v2"
)

func SeedFile() {
	var dbUser models.User
	if res := inits.DB.Find(&dbUser); res.Error != nil {
		log.Fatalf("Failed to find random user: %v", res.Error)
		return
	}

	fileName := "test1.mkv"
	fileSplit := strings.Split(fileName, ".")
	fileExt := fileSplit[len(fileSplit)-1]
	fileByte, err := ioutil.ReadFile("./test/files/test1.mkv")
	if err != nil {
		log.Fatal(err)
	}
	fileId := uuid.NewString()
	filePath := fmt.Sprintf("./videos/%s.%s", fileId, fileExt)

	if err = ioutil.WriteFile(filePath, fileByte, 0777); err != nil {
		log.Fatalf("Failed to save file: %v", err)
		return
	}

	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Failed to open current file")
		return
	}
	defer file.Close()

	fileStat, err := file.Stat()
	if err != nil {
		log.Fatalf("Failed to get stat of current file")
		return
	}

	// create hash
	f, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
		return
	}
	defer f.Close()
	// obtain hash from file
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		log.Fatalf("Failed to copy file: %v", err)
		return
	}
	FileHash := fmt.Sprintf("%x", h.Sum(nil))

	// check file hash with database
	var existingFile models.File
	if res := inits.DB.
		Where(&models.File{
			Hash: FileHash,
		}).First(&existingFile); res.Error == nil {
		// file is dublicate and can be linked
		// delete uploaded file
		os.Remove(filePath)
		// link old uploaded file to new link
		dbLink := models.Link{
			UUID:           uuid.NewString(),
			ParentFolderID: 0,
			UserID:         dbUser.ID,
			FileID:         existingFile.ID,
			Name:           fileName,
		}
		if res := inits.DB.Create(&dbLink); res.Error != nil {
			log.Fatalf("Error saving link in database: %v", res.Error)
			return
		}
	}

	// ffprobe context
	ctx, cancelFn := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFn()

	// probe file
	data, err := ffprobe.ProbeURL(ctx, filePath)
	if err != nil {
		os.Remove(filePath)
		log.Fatalf("Error getting data: %v", err)

		return
	}
	// proobe type
	dataStreams := data.StreamType(ffprobe.StreamAny)

	// declare needed informations
	var videoStream ffprobe.Stream
	var subtitleStreams []ffprobe.Stream
	var audioStreams []ffprobe.Stream
	var avgFramerate float64
	videoDuration := data.Format.Duration().Seconds()

	// loop over streams in file
	for _, streamInfo := range dataStreams {
		if streamInfo.CodecType == "video" {
			videoStream = streamInfo
		}

		if streamInfo.CodecType == "subtitle" && streamInfo.CodecName != "hdmv_pgs_subtitle" {
			subtitleStreams = append(subtitleStreams, streamInfo)
		}

		if streamInfo.CodecType == "audio" {
			audioStreams = append(audioStreams, streamInfo)
		}
	}

	// set average framerate
	if rawAvgFramerateObj := strings.Split(videoStream.AvgFrameRate, "/"); len(rawAvgFramerateObj) == 2 {
		a, errA := strconv.ParseFloat(rawAvgFramerateObj[0], 64)
		b, errB := strconv.ParseFloat(rawAvgFramerateObj[1], 64)
		if a > 0 && b > 0 && errA == nil && errB == nil {
			avgFramerate = a / b
		}
	}

	// declare required variables for database insert
	videoHeight := videoStream.Height
	videoWidth := videoStream.Width

	if videoDuration == 0 {
		os.Remove(filePath)
		log.Fatalf("Error getting videoDuration: %v %v", err, dataStreams)
		return
	}

	// save file data to database
	dbFile := models.File{
		UUID:         fileId,
		Hash:         FileHash,
		Path:         filePath,
		UserID:       dbUser.ID,
		Height:       int64(videoHeight),
		Width:        int64(videoWidth),
		Duration:     videoDuration,
		AvgFrameRate: avgFramerate,
		Size:         fileStat.Size(),
	}
	if res := inits.DB.Create(&dbFile); res.Error != nil {
		os.Remove(filePath)
		log.Fatalf("Error saving file in database: %v", res.Error)
		return
	}

	// save subtitle data to database so they can be converted later
	for index, subtitleStream := range subtitleStreams {
		// generate subtitle name
		var subtitleName = fmt.Sprintf("Subtitle %v", index+1)
		if autoName, err := subtitleStream.TagList.GetString("title"); err == nil && autoName != "" {
			subtitleName = autoName
		}
		// detect subtitle language
		var subtitleLang = "en"
		if autoLang, err := subtitleStream.TagList.GetString("language"); err == nil && autoLang != "" && len(autoLang) < 10 {
			subtitleLang = autoLang
		}

		log.Printf("subtitleName: %s / subtitleLang: %s", subtitleName, subtitleLang)

		// generate unique identifier for subtitle
		subtitleId := uuid.NewString()

		// save subtitle data to database
		dbSubtitle := models.Subtitle{
			UUID:          subtitleId,
			Name:          subtitleName,
			Lang:          subtitleLang,
			Index:         index,
			FileID:        dbFile.ID,
			Path:          fmt.Sprintf("./videos/qualitys/%s/%s", dbFile.UUID, subtitleId),
			OriginalCodec: subtitleStream.CodecName,
			Encoding:      false,
			Failed:        false,
			Ready:         false,
			Error:         "",
		}
		if res := inits.DB.Create(&dbSubtitle); res.Error != nil {
			os.Remove(filePath)
			log.Fatalf("Error saving Subtitle in database: %v", res.Error)
			return
		}
	}

	// save audio data to database so they can be converted later
	for index, audioStream := range audioStreams {
		// generate  audio name
		var audioName = fmt.Sprintf("Audio %v", index+1)
		if autoName, err := audioStream.TagList.GetString("title"); err == nil && autoName != "" {
			audioName = autoName
		}
		// detect  audio language
		var audioLang = "en"
		if autoLang, err := audioStream.TagList.GetString("language"); err == nil && autoLang != "" && len(autoLang) < 10 {
			audioLang = autoLang
		}

		log.Printf(" audioName: %s /  audioLang: %s", audioName, audioLang)

		// generate unique identifier for  audio
		audioId := uuid.NewString()

		// save  audio data to database
		dbAudio := models.Audio{
			UUID:          audioId,
			Name:          audioName,
			Lang:          audioLang,
			Index:         index,
			FileID:        dbFile.ID,
			Path:          fmt.Sprintf("./videos/qualitys/%s/%s", dbFile.UUID, audioId),
			OriginalCodec: audioStream.CodecName,
			Encoding:      false,
			Failed:        false,
			Ready:         false,
			Error:         "",
		}
		if res := inits.DB.Create(&dbAudio); res.Error != nil {
			os.Remove(filePath)
			log.Fatalf("Error saving Audio in database: %v", res.Error)
			return
		}
	}

	// save link data to database
	dbLink := models.Link{
		UUID:           uuid.NewString(),
		Name:           fileName,
		ParentFolderID: 0,
		UserID:         dbUser.ID,
		FileID:         dbFile.ID,
	}
	if res := inits.DB.Create(&dbLink); res.Error != nil {
		log.Fatalf("Error saving link in database: %v", res.Error)
		return
	}

	// add qualitys to database so they can be converted later
	for _, qualityOpt := range models.AvailableQualitys {
		qualityPath := fmt.Sprintf("./videos/qualitys/%s/%s", fileId, qualityOpt.FolderName)
		// switch framerate if too high
		var qualityFrameRate float64 = 0
		if avgFramerate > 30 {
			qualityFrameRate = 30
		}

		if float64(videoWidth/videoHeight) > float64(16/9) {
			// smaller than 16:9 ratio should be fixed by height
			if qualityOpt.Width <= int64(videoWidth) {
				if res := inits.DB.Create(&models.Quality{
					FileID:       dbFile.ID,
					Name:         qualityOpt.Name,
					Width:        int64(math.RoundToEven((float64(videoWidth)/(float64(videoHeight)/float64(qualityOpt.Height)))/2) * 2),
					Height:       int64(math.RoundToEven(float64(qualityOpt.Height)/2) * 2),
					Crf:          qualityOpt.Crf,
					AvgFrameRate: qualityFrameRate,
					Path:         qualityPath,
					OutputFile:   "out.m3u8",
					Encoding:     false,
					Failed:       false,
					Ready:        false,
					Error:        "",
				}); res.Error != nil {
					log.Fatalf("Error saving quality in database: %v\n", res.Error)
					return
				}
			}
		} else {
			// bigger than 16:9 ratio should be fixed by width
			if qualityOpt.Height <= int64(videoHeight) {
				if res := inits.DB.Create(&models.Quality{
					FileID:       dbFile.ID,
					Name:         qualityOpt.Name,
					Width:        int64(math.RoundToEven(float64(qualityOpt.Width)/2) * 2),
					Height:       int64(math.RoundToEven((float64(videoHeight)/(float64(videoWidth)/float64(qualityOpt.Width)))/2) * 2),
					Crf:          qualityOpt.Crf,
					AvgFrameRate: qualityFrameRate,
					Path:         qualityPath,
					OutputFile:   "out.m3u8",
					Encoding:     false,
					Failed:       false,
					Ready:        false,
					Error:        "",
				}); res.Error != nil {
					log.Fatalf("Error saving quality in database: %v\n", res.Error)
					return
				}
			}
		}
	}

	// start and await encoding process
	encworker.ConsoleEncode()
	encworker.ConsoleEncode_audio()
	encworker.ConsoleEncode_sub()
	go encworker.StartEncCleenup()

	// wait until file is deleted
	for {
		log.Println("encoding...")
		if _, err := os.Stat(filePath); err != nil {
			break
		}
		time.Sleep(time.Second)
	}

	// finish
	return
}
