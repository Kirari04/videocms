package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func GetM3u8Data(c *fiber.Ctx) error {
	type Request struct {
		UUID      string  `validate:"required,uuid_rfc4122"`
		AUDIOUUID *string `validate:"required,uuid_rfc4122"`
	}

	var requestValidation Request
	if err := c.ParamsParser(&requestValidation); err != nil {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "none",
				Tag:         "none",
				Value:       "Invalid body request format",
			},
		})
	}

	if errors := helpers.ValidateStruct(requestValidation); len(errors) > 0 {
		return c.Status(400).JSON(errors)
	}

	//translate link id to file id
	var dbLink models.Link
	if dbRes := inits.DB.
		Model(&models.Link{}).
		Preload("File").
		Preload("File.Qualitys").
		Preload("File.Subtitles").
		Preload("File.Audios").
		Where(&models.Link{
			UUID: requestValidation.UUID,
		}).
		First(&dbLink); dbRes.Error != nil {
		return c.Status(fiber.StatusNotFound).SendString("Link doesn't exist")
	}

	//check if contains audio
	var dbAudioPtr *models.Audio
	if requestValidation.AUDIOUUID != nil {
		for _, audio := range dbLink.File.Audios {
			if audio.UUID == *requestValidation.AUDIOUUID {
				dbAudioPtr = &audio
			}
		}
	}

	return c.SendString(getM3u8Stream(&dbLink, &dbLink.File.Qualitys, dbAudioPtr))
}

func getM3u8Stream(dbLink *models.Link, qualitys *[]models.Quality, audio *models.Audio) string {
	m3u8 := "#EXTM3U\n#EXT-X-VERSION:6"
	if audio != nil {
		m3u8 += fmt.Sprintf(
			"\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"AAC\",NAME=\"Subtitle\",LANGUAGE=\"%s\",URI=\"%s\"",
			audio.Lang,
			fmt.Sprintf("/videos/qualitys/%s/%s/audio/audio.m3u8", dbLink.UUID, audio.UUID),
		)
	}
	for _, quality := range *qualitys {
		if quality.Type == "hls" && quality.Ready {
			m3u8 += fmt.Sprintf(
				"\n#EXT-X-STREAM-INF:BANDWIDTH=%d,AUDIO=\"AAC\",RESOLUTION=%s,CODECS=\"avc1.640015,mp4a.40.2\"\n%s",
				int64(quality.Height*quality.Width*20),
				fmt.Sprintf("%dx%d", quality.Height, quality.Height),
				fmt.Sprintf("/videos/qualitys/%s/%s/%s", dbLink.UUID, quality.Name, quality.OutputFile),
			)
		}

	}
	return m3u8
}
