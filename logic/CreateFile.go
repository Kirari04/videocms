package logic

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gopkg.in/vansante/go-ffprobe.v2"
)

func CreateFile(fromFile string, toFolder uint, fileName string, fileId string, fileSize int64, userId uint) (status int, newFile *models.Link, err error) {
	//check if requested folder exists (if set)
	if toFolder > 0 {
		res := inits.DB.First(&models.Folder{}, toFolder)
		if res.Error != nil {
			return fiber.StatusBadRequest, nil, errors.New("parent folder doesn't exist")
		}
	}

	// create hash
	f, err := os.Open(fromFile)
	if err != nil {
		log.Printf("Failed to open file: %v", err)
		return fiber.StatusInternalServerError, nil, errors.New("")
	}
	defer f.Close()

	// obtain hash from file
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		log.Printf("Failed to create hash from file: %v", err)
		return fiber.StatusInternalServerError, nil, errors.New("")
	}
	FileHash := fmt.Sprintf("%x", h.Sum(nil))

	// check file hash with database
	status, newLink, err := CloneFileByHash(FileHash, toFolder, fileName, userId)
	if err == nil {
		return status, newLink, err
	}

	// ffprobe context
	ctx, cancelFn := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFn()

	// probe file
	data, err := ffprobe.ProbeURL(ctx, fromFile)
	if err != nil {
		log.Printf("Error getting data using ffprobe: %v", err)
		return fiber.StatusInternalServerError, nil, errors.New("")
	}
	// proobe type
	dataStreams := data.StreamType(ffprobe.StreamAny)
	dataSubtitleStreams := data.StreamType(ffprobe.StreamSubtitle)
	// declare needed informations
	var videoStream ffprobe.Stream
	var subtitleStreams []ffprobe.Stream
	var audioStreams []ffprobe.Stream
	var avgFramerate float64
	videoDuration := data.Format.Duration().Seconds()
	hasVideoStream := false

	if videoDuration == 0 || videoDuration > 60*60*10 {
		return fiber.StatusBadRequest, nil, errors.New("invalid video duration")
	}

	// loop over streams in file
	for _, streamInfo := range dataStreams {
		if streamInfo.CodecType == "video" {
			videoStream = streamInfo
			hasVideoStream = true
		}

		if streamInfo.CodecType == "audio" {
			audioStreams = append(audioStreams, streamInfo)
		}
	}

	//loop over subtitles in file
	for _, streamInfo := range dataSubtitleStreams {
		if streamInfo.CodecName != "hdmv_pgs_subtitle" {
			subtitleStreams = append(subtitleStreams, streamInfo)
		}
	}

	//check if video stream exists
	if !hasVideoStream {
		return fiber.StatusBadRequest, nil, errors.New("uploaded file doesn't contain any video stream")
	}

	// set average framerate
	if rawAvgFramerateObj := strings.Split(videoStream.AvgFrameRate, "/"); len(rawAvgFramerateObj) == 2 {
		a, errA := strconv.ParseFloat(rawAvgFramerateObj[0], 64)
		b, errB := strconv.ParseFloat(rawAvgFramerateObj[1], 64)
		if a > 0 && b > 0 && errA == nil && errB == nil {
			avgFramerate = a / b
		}
	}

	// check average framerate
	if avgFramerate < 1 || avgFramerate > 120 {
		return fiber.StatusBadRequest, nil, errors.New("invalid video framerate")
	}

	// check video stream data (resolution)
	if videoStream.Height == 0 || videoStream.Width == 0 {
		log.Printf(
			"Error getting valid videoStream data: type: %v size: %vx%v",
			videoStream.CodecType,
			videoStream.Width,
			videoStream.Height,
		)
		return fiber.StatusInternalServerError, nil, errors.New("")
	}

	// check if resolution is in scope of supported sizes
	if videoStream.Height > 8000 || videoStream.Width > 8000 {
		return fiber.StatusBadRequest, nil, errors.New("video resolution is too high")
	}
	if videoStream.Height < 50 || videoStream.Width < 50 {
		return fiber.StatusBadRequest, nil, errors.New("video resolution is too low")
	}

	// declare required variables for database insert
	videoHeight := videoStream.Height
	videoWidth := videoStream.Width

	if videoDuration == 0 {
		log.Printf("Error getting videoDuration: %v %v", err, dataStreams)
		return fiber.StatusInternalServerError, nil, errors.New("")
	}

	thumbnailFileName := "4x4.webp"
	if responseStatus, err := CreateThumbnail(
		4,
		fromFile,
		1080,
		thumbnailFileName,
		fmt.Sprintf("./videos/qualitys/%s", fileId),
		videoDuration,
		avgFramerate,
	); err != nil {
		log.Printf("Failed to generate thumbnail: %v", err)
		return responseStatus, nil, err
	}

	// save file data to database
	dbFile := models.File{
		UUID:         fileId,
		Hash:         FileHash,
		Thumbnail:    thumbnailFileName,
		Path:         fromFile,
		Folder:       fmt.Sprintf("./videos/qualitys/%s", fileId),
		UserID:       userId,
		Height:       int64(videoHeight),
		Width:        int64(videoWidth),
		Duration:     videoDuration,
		AvgFrameRate: avgFramerate,
		Size:         fileSize,
	}
	if res := inits.DB.Create(&dbFile); res.Error != nil {
		log.Printf("Error saving file in database: %v", res.Error)
		return fiber.StatusInternalServerError, nil, errors.New("")
	}

	// save subtitle data to database so they can be converted later
	for index, subtitleStream := range subtitleStreams {
		// generate subtitle name
		var subtitleName = fmt.Sprintf("Subtitle %v", index+1)
		if autoName := subtitleStream.Tags.Title; autoName != "" && len(autoName) < 20 {
			subtitleName = autoName
		}

		// detect subtitle language
		var subtitleLang = "eng"
		if autoLang := subtitleStream.Tags.Language; autoLang != "" && len(autoLang) < 10 {
			subtitleLang = autoLang
		}

		// log.Printf("subtitleName: %s / subtitleLang: %s", subtitleStream.Tags.Title, subtitleStream.Tags.Language)

		for _, subOpt := range models.AvailableSubtitles {
			// generate unique identifier for subtitle
			subtitleId := uuid.NewString()

			// save subtitle data to database
			dbSubtitle := models.Subtitle{
				UUID:          subtitleId,
				Name:          subtitleName,
				Lang:          subtitleLang,
				Index:         index,
				Codec:         subOpt.Codec,
				Type:          subOpt.Type,
				OutputFile:    subOpt.OutputFile,
				FileID:        dbFile.ID,
				Path:          fmt.Sprintf("./videos/qualitys/%s/%s", dbFile.UUID, subtitleId),
				OriginalCodec: subtitleStream.CodecName,
				Encoding:      false,
				Failed:        false,
				Ready:         false,
				Error:         "",
			}
			if res := inits.DB.Create(&dbSubtitle); res.Error != nil {
				log.Printf("Error saving Subtitle in database: %v", res.Error)
				return fiber.StatusInternalServerError, nil, errors.New("")
			}
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

		// log.Printf(" audioName: %s /  audioLang: %s", audioName, audioLang)

		for _, audioOpt := range models.AvailableAudios {
			// generate unique identifier for  audio
			audioId := uuid.NewString()

			// save  audio data to database
			if res := inits.DB.Create(&models.Audio{
				UUID:          audioId,
				Name:          audioName,
				Lang:          audioLang,
				Index:         index,
				Codec:         audioOpt.Codec,
				Type:          audioOpt.Type,
				OutputFile:    audioOpt.OutputFile,
				FileID:        dbFile.ID,
				Path:          fmt.Sprintf("./videos/qualitys/%s/%s", dbFile.UUID, audioId),
				OriginalCodec: audioStream.CodecName,
				Encoding:      false,
				Failed:        false,
				Ready:         false,
				Error:         "",
			}); res.Error != nil {
				log.Printf("Error saving Audio in database: %v", res.Error)
				return fiber.StatusInternalServerError, nil, errors.New("")
			}
		}
	}

	// save link data to database
	dbLink := models.Link{
		UUID:           uuid.NewString(),
		Name:           fileName,
		ParentFolderID: toFolder,
		UserID:         userId,
		FileID:         dbFile.ID,
	}
	if res := inits.DB.Create(&dbLink); res.Error != nil {
		log.Printf("Error saving link in database: %v", res.Error)
		return fiber.StatusInternalServerError, nil, errors.New("")
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
					Type:         qualityOpt.Type,
					Muted:        qualityOpt.Muted,
					AudioCodec:   qualityOpt.AudioCodec,
					AvgFrameRate: qualityFrameRate,
					Path:         qualityPath,
					OutputFile:   qualityOpt.OutputFile,
					Encoding:     false,
					Failed:       false,
					Ready:        false,
					Error:        "",
				}); res.Error != nil {
					log.Printf("Error saving quality in database: %v\n", res.Error)
					return fiber.StatusInternalServerError, nil, errors.New("")
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
					Type:         qualityOpt.Type,
					Muted:        qualityOpt.Muted,
					AudioCodec:   qualityOpt.AudioCodec,
					AvgFrameRate: qualityFrameRate,
					Path:         qualityPath,
					OutputFile:   qualityOpt.OutputFile,
					Encoding:     false,
					Failed:       false,
					Ready:        false,
					Error:        "",
				}); res.Error != nil {
					log.Printf("Error saving quality in database: %v\n", res.Error)
					return fiber.StatusInternalServerError, nil, errors.New("")
				}
			}
		}
	}

	// return link to file
	return fiber.StatusOK, &dbLink, nil
}
