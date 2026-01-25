package logic

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"net/http"
	"regexp"
)

func GetAudioData(requestValidation *models.AudioGetValidation) (status int, filePath *string, userID uint, fileID uint, audioID uint, err error) {
	reFILE := regexp.MustCompile(`^audio[0-9]{0,4}\.(m3u8|ts|wav|mp3|ogg)$`)

	if !reFILE.MatchString(requestValidation.FILE) {
		return http.StatusBadRequest, nil, 0, 0, 0, errors.New("bad file format")
	}

	//translate link id to file id
	var dbLink models.Link

	if dbRes := inits.DB.
		Model(&models.Link{}).
		Preload("File").
		Preload("File.Audios").
		Where(&models.Link{
			UUID: requestValidation.UUID,
		}).
		First(&dbLink); dbRes.Error != nil {
		return http.StatusNotFound, nil, 0, 0, 0, errors.New("audio doesn't exist")
	}

	//check if audio uuid exists
	audioExists := false
	for _, audio := range dbLink.File.Audios {
		if audio.Ready &&
			audio.UUID == requestValidation.AUDIOUUID {
			audioExists = true
			audioID = audio.ID
		}
	}
	if !audioExists {
		return http.StatusNotFound, nil, 0, 0, 0, errors.New("audio doesn't exist")
	}

	resPath := fmt.Sprintf("%s/%s/%s/%s", config.ENV.FolderVideoQualitysPriv, dbLink.File.UUID, requestValidation.AUDIOUUID, requestValidation.FILE)
	return http.StatusOK, &resPath, dbLink.UserID, dbLink.FileID, audioID, nil
}
