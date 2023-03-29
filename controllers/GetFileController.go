package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
)

func GetFile(c *fiber.Ctx) error {
	// parse & validate request
	var fileValidation models.LinkGetValidation
	if err := c.QueryParser(&fileValidation); err != nil {
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

	userID := c.Locals("UserID").(uint)

	// query all files
	var link models.Link
	if res := inits.DB.
		Model(&models.Link{}).
		Preload("User").
		Preload("File").
		Preload("File.Qualitys").
		Preload("File.Subtitles").
		Preload("File.Audios").
		Where(&models.Link{
			UserID: userID,
		}).
		First(&link, fileValidation.LinkID); res.Error != nil {
		return c.SendStatus(fiber.StatusNotFound)
	}
	type RespQuali struct {
		Name         string
		Type         string
		Height       int64
		Width        int64
		AvgFrameRate float64
		Ready        bool
		Failed       bool
		Progress     float64
	}
	type RespSub struct {
		Name  string
		Type  string
		Lang  string
		Ready bool
	}
	type RespAudio struct {
		Name  string
		Type  string
		Lang  string
		Ready bool
	}
	type Resp struct {
		CreatedAt      time.Time
		UpdatedAt      time.Time
		UUID           string
		Name           string
		Thumbnail      string
		ParentFolderID uint
		Size           int64
		Duration       float64
		Qualitys       []RespQuali
		Subtitles      []RespSub
		Audios         []RespAudio
	}
	var Qualitys []RespQuali
	for _, Quality := range link.File.Qualitys {
		avgFps := Quality.AvgFrameRate
		if avgFps == 0 {
			avgFps = link.File.AvgFrameRate
		}
		Qualitys = append(Qualitys, RespQuali{
			Name:         Quality.Name,
			Type:         Quality.Type,
			Height:       Quality.Height,
			Width:        Quality.Width,
			AvgFrameRate: avgFps,
			Ready:        Quality.Ready,
			Progress:     Quality.Progress * 100,
			Failed:       Quality.Failed,
		})
	}

	var Subtitles []RespSub
	for _, Subtitle := range link.File.Subtitles {
		Subtitles = append(Subtitles, RespSub{
			Name:  Subtitle.Name,
			Lang:  Subtitle.Lang,
			Type:  Subtitle.Type,
			Ready: Subtitle.Ready,
		})
	}

	var Audios []RespAudio
	for _, Audio := range link.File.Audios {
		Audios = append(Audios, RespAudio{
			Name:  Audio.Name,
			Lang:  Audio.Lang,
			Type:  Audio.Type,
			Ready: Audio.Ready,
		})
	}

	response := Resp{
		CreatedAt:      link.CreatedAt,
		UpdatedAt:      link.UpdatedAt,
		UUID:           link.UUID,
		Name:           link.Name,
		Thumbnail:      fmt.Sprintf("/videos/qualitys/%s/image/thumb/%s", link.UUID, link.File.Thumbnail),
		ParentFolderID: link.ParentFolderID,
		Size:           link.File.Size,
		Duration:       link.File.Duration,
		Qualitys:       Qualitys,
		Subtitles:      Subtitles,
		Audios:         Audios,
	}
	// return value
	return c.JSON(response)
}
