package logic

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"

	"github.com/gofiber/fiber/v2"
)

type GetM3u8DataRequest struct {
	UUID      string `validate:"required,uuid_rfc4122"`
	AUDIOUUID string `validate:"required,uuid_rfc4122"`
}

type GetM3u8DataRequestMuted struct {
	UUID string `validate:"required,uuid_rfc4122"`
}

func GetM3u8Data(UUID string, AUDIOUUID string) (status int, m3u8Str *string, err error) {
	//translate link id to file id
	var dbLink models.Link
	if dbRes := inits.DB.
		Model(&models.Link{}).
		Preload("File").
		Preload("File.Qualitys").
		Preload("File.Audios").
		Where(&models.Link{
			UUID: UUID,
		}).
		First(&dbLink); dbRes.Error != nil {
		return fiber.StatusNotFound, nil, errors.New("link doesn't exist")
	}

	//check if contains audio
	var dbAudioPtr *models.Audio
	if AUDIOUUID != "" {
		for _, audio := range dbLink.File.Audios {
			if audio.Ready &&
				audio.UUID == AUDIOUUID &&
				audio.Type == "hls" {
				dbAudioPtr = &audio
				break
			}
		}
	}
	m3u8Response := helpers.GenM3u8Stream(&dbLink, &dbLink.File.Qualitys, dbAudioPtr)
	return fiber.StatusOK, &m3u8Response, nil
}