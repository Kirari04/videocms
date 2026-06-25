package logic

import (
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"net/http"
)

func (s *Service) GetThumbnailData(fileName string, UUID string) (status int, filePath *string, userID uint, fileID uint, err error) {
	//translate link id to file id
	var dbLink models.Link
	if dbRes := s.Deps.DB.
		Model(&models.Link{}).
		Preload("File").
		Where(&models.Link{
			UUID: UUID,
		}).
		First(&dbLink); dbRes.Error != nil {
		return http.StatusNotFound, nil, 0, 0, errors.New("thumbnail doesn't exist")
	}

	if !s.thumbnailFileAllowedForLink(fileName, dbLink) {
		return http.StatusNotFound, nil, 0, 0, errors.New("thumbnail doesn't exist")
	}

	fileRes := fmt.Sprintf("%s/%s/%s", s.Config().FolderVideoQualitysPriv, dbLink.File.UUID, fileName)

	return http.StatusOK, &fileRes, dbLink.UserID, dbLink.FileID, nil
}
