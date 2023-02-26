package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"context"
	"fmt"
	"log"
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

	fileId := uuid.New()
	fileSplit := strings.Split(file.Filename, ".")
	fileExt := fileSplit[len(fileSplit)-1]
	filePath := fmt.Sprintf("./videos/%s.%s", fileId, fileExt)

	// Save file to storage
	if err := c.SaveFile(file, filePath); err != nil {
		log.Printf("Failed to save file: %v", err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	ctx, cancelFn := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFn()

	data, err := ffprobe.ProbeURL(ctx, filePath)
	if err != nil {
		log.Printf("Error getting data: %v", err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	videoInfo := data.StreamType(ffprobe.StreamVideo)

	videoHeight := videoInfo[len(videoInfo)-1].Height
	videoWidth := videoInfo[len(videoInfo)-1].Width
	videoDuration, err := strconv.ParseFloat(videoInfo[len(videoInfo)-1].Duration, 64)
	if err != nil {
		log.Printf("Error getting videoDuration: %v", err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	// save file to database
	dbFile := models.File{
		Name:     fileValidation.Name,
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
	// save link
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

	// add qualitys to database
	for _, qualityOpt := range models.AvailableQualitys {
		log.Printf("Adding %vx%v", qualityOpt.Width, qualityOpt.Height)
		qualityPath := fmt.Sprintf("./videos/qualitys/%s/%s", fileId, qualityOpt.FolderName)
		if videoHeight > videoWidth {
			// vertical -> compare height
			if qualityOpt.Height <= int64(videoHeight) {
				if res := inits.DB.Create(&models.Quality{
					FileID:   dbFile.ID,
					Name:     qualityOpt.Name,
					Width:    int64(float64(videoWidth) / (float64(videoHeight) / float64(qualityOpt.Height))),
					Height:   qualityOpt.Height,
					Crf:      qualityOpt.Crf,
					Path:     qualityPath,
					Encoding: false,
					Failed:   false,
					Ready:    false,
					Error:    "",
				}); res.Error != nil {
					log.Printf("Error saving quality in database: %v\n", res.Error)
					return c.SendStatus(fiber.StatusInternalServerError)
				}
			}
		} else {
			//horizontal -> compare width
			if qualityOpt.Width <= int64(videoWidth) {
				if res := inits.DB.Create(&models.Quality{
					FileID:   dbFile.ID,
					Name:     qualityOpt.Name,
					Width:    qualityOpt.Width,
					Height:   int64(float64(videoHeight) / (float64(videoWidth) / float64(qualityOpt.Width))),
					Crf:      qualityOpt.Crf,
					Path:     qualityPath,
					Encoding: false,
					Failed:   false,
					Ready:    false,
					Error:    "",
				}); res.Error != nil {
					log.Printf("Error saving quality in database: %v\n", res.Error)
					return c.SendStatus(fiber.StatusInternalServerError)
				}
			}
		}
	}

	return c.Status(fiber.StatusOK).JSON(dbLink)
}
