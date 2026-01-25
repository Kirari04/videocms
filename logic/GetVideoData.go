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

func GetVideoData(fileName string, qualityName string, UUID string) (status int, filePath *string, userID uint, fileID uint, qualityID uint, err error) {
	reQUALITY := regexp.MustCompile(`^([0-9]{3,4}p|(h264))$`)
	reFILE := regexp.MustCompile(`^out[0-9]{0,4}\.(m3u8|ts|webm|mp4)$`)

	if !reQUALITY.MatchString(qualityName) {
		return http.StatusBadRequest, nil, 0, 0, 0, errors.New("bad quality format")
	}

	if !reFILE.MatchString(fileName) {
		return http.StatusBadRequest, nil, 0, 0, 0, errors.New("bad file format")
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
		return http.StatusNotFound, nil, 0, 0, 0, errors.New("video doesn't exist")
	}

	// find quality id
	var dbQuality models.Quality
	if dbRes := inits.DB.
		Model(&models.Quality{}).
		Where(&models.Quality{
			FileID: dbLink.FileID,
			Name:   qualityName,
		}).
		First(&dbQuality); dbRes.Error == nil {
		qualityID = dbQuality.ID
	}

	fileRes := fmt.Sprintf("%s/%s/%s/%s", config.ENV.FolderVideoQualitysPriv, dbLink.File.UUID, qualityName, fileName)
	return http.StatusOK, &fileRes, dbLink.File.UserID, dbLink.FileID, qualityID, nil
}
