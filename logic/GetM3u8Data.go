package logic

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/models"
	"errors"
	"net/http"
)

type GetM3u8DataRequest struct {
	UUID      string `validate:"required,uuid_rfc4122"`
	AUDIOUUID string `validate:"omitempty,uuid_rfc4122"`
}

type GetM3u8DataRequestMuted struct {
	UUID string `validate:"required,uuid_rfc4122"`
}

func (s *Service) GetM3u8Data(UUID string, AUDIOUUID string) (status int, m3u8Str *string, userID uint, fileID uint, audioID uint, err error) {
	//translate link id to file id
	var dbLink models.Link
	if dbRes := s.Deps.DB.
		Model(&models.Link{}).
		Preload("File").
		Preload("File.Qualitys").
		Preload("File.Audios").
		Where(&models.Link{
			UUID: UUID,
		}).
		First(&dbLink); dbRes.Error != nil {
		return http.StatusNotFound, nil, 0, 0, 0, errors.New("link doesn't exist")
	}

	//check if contains audio
	var dbAudioPtr *models.Audio
	if AUDIOUUID != "" {
		for _, audio := range dbLink.File.Audios {
			if audio.Ready &&
				audio.UUID == AUDIOUUID &&
				audio.Type == "hls" {
				dbAudioPtr = &audio
				audioID = audio.ID
				break
			}
		}
	}
	m3u8Response := helpers.GenM3u8Stream(s.Config().FolderVideoQualitysPub, &dbLink, &dbLink.File.Qualitys, dbAudioPtr)
	return http.StatusOK, &m3u8Response, dbLink.UserID, dbLink.FileID, audioID, nil
}

func (s *Service) GetM3u8DataMulti(UUID string) (status int, m3u8Str *string, userID uint, fileID uint, err error) {
	//translate link id to file id
	var dbLink models.Link
	if dbRes := s.Deps.DB.
		Model(&models.Link{}).
		Preload("File").
		Preload("File.Qualitys").
		Preload("File.Audios").
		Where(&models.Link{
			UUID: UUID,
		}).
		First(&dbLink); dbRes.Error != nil {
		return http.StatusNotFound, nil, 0, 0, errors.New("link doesn't exist")
	}

	m3u8Response := helpers.GenM3u8StreamMulti(s.Config().FolderVideoQualitysPub, &dbLink, &dbLink.File.Qualitys, &dbLink.File.Audios)
	return http.StatusOK, &m3u8Response, dbLink.UserID, dbLink.FileID, nil
}
