package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"context"
	"fmt"
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
	var videoDuration = data.Format.Duration().Seconds()
	hasVideoStream := false

	// loop over streams in file
	for _, streamInfo := range dataStreams {
		if streamInfo.CodecType == "video" {
			videoStream = streamInfo
			hasVideoStream = true
		}

		if streamInfo.CodecType == "subtitle" {
			subtitleStreams = append(subtitleStreams, streamInfo)
		}

		// get any duration time (subtitles may have one too => on mkvs)
		if streamInfo.Duration != "" && videoDuration == 0 {
			videoDuration, _ = strconv.ParseFloat(streamInfo.Duration, 64)
		}

		// get video duration (usually webm files)
		if tmpDuration, err := streamInfo.TagList.GetString("DURATION"); err == nil && tmpDuration != "" && videoDuration == 0 {
			log.Printf("tmpDuration: %v", tmpDuration)
			var hours float64
			var minutes float64
			var seconds float64
			tmpDurationSlices := strings.Split(tmpDuration, ":")
			if len(tmpDurationSlices) == 3 {
				hours, _ = strconv.ParseFloat(tmpDurationSlices[0], 64)
				minutes, _ = strconv.ParseFloat(tmpDurationSlices[1], 64)
				seconds, _ = strconv.ParseFloat(tmpDurationSlices[2], 64)
				videoDuration = seconds + (minutes * 60) + (hours * 60 * 60)
			}

		}
	}

	//check if video stream exists
	if !hasVideoStream {
		os.Remove(filePath)
		return c.SendStatus(fiber.StatusBadRequest)
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
		Name:     fileValidation.Name,
		UUID:     fileId,
		Path:     filePath,
		UserID:   c.Locals("UserID").(uint),
		Height:   int64(videoHeight),
		Width:    int64(videoWidth),
		Duration: videoDuration,
		Size:     file.Size,
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
			UUID:     subtitleId,
			Name:     subtitleName,
			Lang:     subtitleLang,
			Index:    index,
			FileID:   dbFile.ID,
			Path:     fmt.Sprintf("./videos/qualitys/%s/%s", dbFile.UUID, subtitleId),
			Encoding: false,
			Failed:   false,
			Ready:    false,
			Error:    "",
		}
		if res := inits.DB.Create(&dbSubtitle); res.Error != nil {
			log.Printf("Error saving Subtitle in database: %v", res.Error)
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
		if videoHeight > videoWidth {
			// vertical -> compare height
			if qualityOpt.Height <= int64(videoHeight) {
				if res := inits.DB.Create(&models.Quality{
					FileID:     dbFile.ID,
					Name:       qualityOpt.Name,
					Width:      int64(math.RoundToEven((float64(videoWidth)/(float64(videoHeight)/float64(qualityOpt.Height)))/2) * 2),
					Height:     int64(math.RoundToEven(float64(qualityOpt.Height)/2) * 2),
					Crf:        qualityOpt.Crf,
					Path:       qualityPath,
					OutputFile: "out.m3u8",
					Encoding:   false,
					Failed:     false,
					Ready:      false,
					Error:      "",
				}); res.Error != nil {
					log.Printf("Error saving quality in database: %v\n", res.Error)
					return c.SendStatus(fiber.StatusInternalServerError)
				}
			}
		} else {
			//horizontal -> compare width
			if qualityOpt.Width <= int64(videoWidth) {
				if res := inits.DB.Create(&models.Quality{
					FileID:     dbFile.ID,
					Name:       qualityOpt.Name,
					Width:      int64(math.RoundToEven(float64(qualityOpt.Width)/2) * 2),
					Height:     int64(math.RoundToEven((float64(videoHeight)/(float64(videoWidth)/float64(qualityOpt.Width)))/2) * 2),
					Crf:        qualityOpt.Crf,
					Path:       qualityPath,
					OutputFile: "out.m3u8",
					Encoding:   false,
					Failed:     false,
					Ready:      false,
					Error:      "",
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
