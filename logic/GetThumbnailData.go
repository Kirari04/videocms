package logic

import (
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"net/http"
)

type ThumbnailObject struct {
	FileUUID string
	StoreID  string
	PoolID   uint
	UserID   uint
	FileID   uint
}

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
	status, object, err := s.ResolveThumbnailObject(fileName, UUID)
	if err != nil {
		return status, "", 0, 0, err
	}
	return status, object.FileUUID, object.UserID, object.FileID, nil
}

func (s *Service) ResolveThumbnailObject(fileName string, UUID string) (status int, object ThumbnailObject, err error) {
	//translate link id to file id
	var dbLink models.Link
	if dbRes := s.Deps.DB.
		Model(&models.Link{}).
		Preload("File").
		Where(&models.Link{
			UUID: UUID,
		}).
		First(&dbLink); dbRes.Error != nil {
		return http.StatusNotFound, ThumbnailObject{}, errors.New("thumbnail doesn't exist")
	}

	if !s.thumbnailFileAllowedForLink(fileName, dbLink) {
		return http.StatusNotFound, ThumbnailObject{}, errors.New("thumbnail doesn't exist")
	}

	return http.StatusOK, ThumbnailObject{
		FileUUID: dbLink.File.UUID,
		StoreID:  dbLink.File.StorageID,
		PoolID:   valueOrZeroUint(dbLink.File.StoragePoolID),
		UserID:   dbLink.UserID,
		FileID:   dbLink.FileID,
	}, nil
}

func valueOrZeroUint(value *uint) uint {
	if value == nil {
		return 0
	}
	return *value
}
