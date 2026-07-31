package logic

import (
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"net/http"
)

func (s *Service) GetThumbnailData(fileName string, UUID string) (status int, filePath *string, userID uint, fileID uint, err error) {
	status, fileUUID, userID, fileID, err := s.ResolveThumbnailData(fileName, UUID)
	if err != nil {
		return status, nil, 0, 0, err
	}
	fileRes := fmt.Sprintf("%s/%s/%s", s.Config().FolderVideoQualitysPriv, fileUUID, fileName)
	return status, &fileRes, userID, fileID, nil
}

// ResolveThumbnailData authorizes a thumbnail name and returns its logical
// file identity without exposing a storage-provider path.
func (s *Service) ResolveThumbnailData(fileName string, UUID string) (status int, fileUUID string, userID uint, fileID uint, err error) {
	//translate link id to file id
	var dbLink models.Link
	if dbRes := s.Deps.DB.
		Model(&models.Link{}).
		Preload("File").
		Where(&models.Link{
			UUID: UUID,
		}).
		First(&dbLink); dbRes.Error != nil {
		return http.StatusNotFound, "", 0, 0, errors.New("thumbnail doesn't exist")
	}

	if !s.thumbnailFileAllowedForLink(fileName, dbLink) {
		return http.StatusNotFound, "", 0, 0, errors.New("thumbnail doesn't exist")
	}

	return http.StatusOK, dbLink.File.UUID, dbLink.UserID, dbLink.FileID, nil
}
