package logic

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/models"
)

type Service struct {
	Deps *app.Deps
}

func NewService(deps *app.Deps) *Service {
	return &Service{Deps: deps}
}

func (s *Service) Config() config.Config {
	return s.Deps.Config()
}

func (s *Service) Qualities() []models.AvailableQuality {
	return s.Deps.Qualities()
}

func (s *Service) TrackTraffic(userID, fileID, qualityID, audioID uint, bytes uint64) {
	if bytes == 0 {
		return
	}
	s.Deps.DB.Create(&models.TrafficLog{
		UserID:    userID,
		FileID:    fileID,
		QualityID: qualityID,
		AudioID:   audioID,
		Bytes:     bytes,
	})
}

func (s *Service) TrackUpload(userID uint, fileID uint, uploadSessionID uint, bytes uint64) {
	if bytes == 0 {
		return
	}
	s.Deps.DB.Create(&models.UploadLog{
		UserID:          userID,
		FileID:          fileID,
		UploadSessionID: uploadSessionID,
		Bytes:           bytes,
	})
}

func (s *Service) TrackEncoding(userID uint, fileID uint, taskType string, seconds float64) {
	if seconds <= 0 {
		return
	}
	s.Deps.DB.Create(&models.EncodingLog{
		UserID:  userID,
		FileID:  fileID,
		Type:    taskType,
		Seconds: seconds,
	})
}

func (s *Service) GetModelUser(userID uint) (*models.User, error) {
	var user models.User
	if res := s.Deps.DB.First(&user, userID); res.Error != nil {
		return nil, res.Error
	}
	return &user, nil
}
