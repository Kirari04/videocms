package controllers

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"context"
	"crypto/sha256"
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

func CreateFile(c *fiber.Ctx) error {
	if config.ENV.UploadEnabled == "false" {
		return c.Status(fiber.StatusServiceUnavailable).SendString("Upload has been desabled")
	}

	// parse & validate request
	var fileValidation models.FileCreateValidation
	if err := c.BodyParser(&fileValidation); err != nil {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "none",
				Tag:         "none",
				Value:       "Invalid body request format",
			},
		})
	}

	if errors := helpers.ValidateStruct(fileValidation); len(errors) > 0 {
		return c.Status(400).JSON(errors)
	}

	//check if requested folder exists (if set)
	if fileValidation.ParentFolderID > 0 {
		res := inits.DB.First(&models.Folder{}, fileValidation.ParentFolderID)
		if res.Error != nil {
			return c.Status(400).JSON([]helpers.ValidationError{
				{
					FailedField: "ParentFolderID",
					Tag:         "exists",
					Value:       "Parent folder doesn't exist",
				},
			})
		}
	}

	file, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("No File uploaded")
	}

	fileId := uuid.NewString()
	fileSplit := strings.Split(file.Filename, ".")
	fileExt := fileSplit[len(fileSplit)-1]
	filePath := fmt.Sprintf("./videos/%s.%s", fileId, fileExt)

	// Save file to storage
	if err := c.SaveFile(file, filePath); err != nil {
		log.Printf("Failed to save file: %v", err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	// create hash
	f, err := os.Open(filePath)
	if err != nil {
		log.Printf("Failed to open file: %v", err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		log.Printf("Failed to copy file: %v", err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	FileHash := fmt.Sprintf("%x", h.Sum(nil))

	var existingFile models.File
	if res := inits.DB.
		Where(&models.File{
			Hash: FileHash,
		}).First(&existingFile); res.Error == nil {
		// file exists
		dbLink := models.Link{
			UUID:           uuid.NewString(),
			ParentFolderID: fileValidation.ParentFolderID,
			UserID:         c.Locals("UserID").(uint),
			FileID:         existingFile.ID,
		}
		if res := inits.DB.Create(&dbLink); res.Error != nil {
			log.Printf("Error saving link in database: %v", res.Error)
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		return c.Status(fiber.StatusOK).JSON(dbLink)
	}

	// ffprobe context
	ctx, cancelFn := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFn()

	// probe file
	data, err := ffprobe.ProbeURL(ctx, filePath)
	if err != nil {
		log.Printf("Error getting data: %v", err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	// proobe type
	dataStreams := data.StreamType(ffprobe.StreamAny)

	// declare needed informations
	var videoStream ffprobe.Stream
	var subtitleStreams []ffprobe.Stream
	var audioStreams []ffprobe.Stream
	var avgFramerate float64
	videoDuration := data.Format.Duration().Seconds()
	hasVideoStream := false

	if videoDuration == 0 || videoDuration > 60*60*10 {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid video duration")
	}

	// loop over streams in file
	for _, streamInfo := range dataStreams {
		if streamInfo.CodecType == "video" {
			videoStream = streamInfo
			hasVideoStream = true
		}

		if streamInfo.CodecType == "subtitle" && streamInfo.CodecName != "hdmv_pgs_subtitle" {
			subtitleStreams = append(subtitleStreams, streamInfo)
		}

		if streamInfo.CodecType == "audio" {
			audioStreams = append(audioStreams, streamInfo)
		}
	}

	//check if video stream exists
	if !hasVideoStream {
		os.Remove(filePath)
		return c.SendStatus(fiber.StatusBadRequest)
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
		return c.Status(fiber.StatusBadRequest).SendString("Invalid video framerate")
	}

	// check video stream data
	if videoStream.Height == 0 || videoStream.Width == 0 {
		log.Printf(
			"Error getting valid videoStream data: type: %v size: %vx%v",
			videoStream.CodecType,
			videoStream.Width,
			videoStream.Height,
		)
		os.Remove(filePath)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	// declare required variables for database insert
	videoHeight := videoStream.Height
	videoWidth := videoStream.Width

	if videoDuration == 0 {
		log.Printf("Error getting videoDuration: %v %v", err, dataStreams)
		os.Remove(filePath)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	// save file data to database
	dbFile := models.File{
		Name:         fileValidation.Name,
		UUID:         fileId,
		Hash:         FileHash,
		Path:         filePath,
		UserID:       c.Locals("UserID").(uint),
		Height:       int64(videoHeight),
		Width:        int64(videoWidth),
		Duration:     videoDuration,
		AvgFrameRate: avgFramerate,
		Size:         file.Size,
	}
	if res := inits.DB.Create(&dbFile); res.Error != nil {
		log.Printf("Error saving file in database: %v", res.Error)
		os.Remove(filePath)
		return c.SendStatus(fiber.StatusInternalServerError)
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
			log.Printf("Error saving Subtitle in database: %v", res.Error)
			os.Remove(filePath)
			return c.SendStatus(fiber.StatusInternalServerError)
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
			log.Printf("Error saving Audio in database: %v", res.Error)
			os.Remove(filePath)
			return c.SendStatus(fiber.StatusInternalServerError)
		}
	}

	// save link data to database
	dbLink := models.Link{
		UUID:           uuid.NewString(),
		ParentFolderID: fileValidation.ParentFolderID,
		UserID:         c.Locals("UserID").(uint),
		FileID:         dbFile.ID,
	}
	if res := inits.DB.Create(&dbLink); res.Error != nil {
		log.Printf("Error saving link in database: %v", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	// add qualitys to database so they can be converted later
	for _, qualityOpt := range models.AvailableQualitys {
		qualityPath := fmt.Sprintf("./videos/qualitys/%s/%s", fileId, qualityOpt.FolderName)
		// switch framerate if too high
		var qualityFrameRate float64 = 0
		if avgFramerate > 30 {
			qualityFrameRate = 30
		}

		if videoHeight > videoWidth {
			// vertical -> compare height
			if qualityOpt.Width <= int64(videoWidth) {
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
					log.Printf("Error saving quality in database: %v\n", res.Error)
					return c.SendStatus(fiber.StatusInternalServerError)
				}
			}
		} else {
			//horizontal -> compare width
			if qualityOpt.Height <= int64(videoHeight) {
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
					log.Printf("Error saving quality in database: %v\n", res.Error)
					return c.SendStatus(fiber.StatusInternalServerError)
				}
			}
		}
	}

	// return link to file
	return c.Status(fiber.StatusOK).JSON(dbLink)
}
