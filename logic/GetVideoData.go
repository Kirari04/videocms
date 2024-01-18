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

func GetVideoData(fileName string, qualityName string, UUID string) (status int, filePath *string, err error) {
	reQUALITY := regexp.MustCompile(`^([0-9]{3,4}p|(h264|vp9|av1))$`)
	reFILE := regexp.MustCompile(`^out[0-9]{0,4}\.(m3u8|ts|webm|mp4)$`)

	if !reQUALITY.MatchString(qualityName) {
		return http.StatusBadRequest, nil, errors.New("bad quality format")
	}

	if !reFILE.MatchString(fileName) {
		return http.StatusBadRequest, nil, errors.New("bad file format")
	}

	//translate link id to file id
	var dbLink models.Link
	if dbRes := inits.DB.
		Model(&models.Link{}).
		Preload("File").
		Where(&models.Link{
			UUID: UUID,
		}).
		First(&dbLink); dbRes.Error != nil {
		return http.StatusNotFound, nil, errors.New("video doesn't exist")
	}

	fileRes := fmt.Sprintf("%s/%s/%s/%s", config.ENV.FolderVideoQualitysPriv, dbLink.File.UUID, qualityName, fileName)
	return http.StatusOK, &fileRes, nil
}
