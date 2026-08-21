package logic

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/traffic"
	"log"
	"time"
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
	s.trackTraffic(userID, fileID, qualityID, audioID, models.TrafficSourcePlayer, bytes, 0, "", "")
}

func (s *Service) TrackDownloadTraffic(userID, fileID, qualityID uint, bytes uint64) {
	s.trackTraffic(userID, fileID, qualityID, 0, models.TrafficSourceDownload, bytes, 0, "", "")
}

func (s *Service) TrackStorageTraffic(
	userID, fileID, qualityID, audioID uint,
	bytes uint64,
	storagePoolID uint,
	storageMountUUID string,
	cacheHit bool,
) {
	deliverySource := models.TrafficDeliverySourceOrigin
	if cacheHit {
		deliverySource = models.TrafficDeliverySourceCache
	}
	s.trackTraffic(
		userID, fileID, qualityID, audioID, models.TrafficSourcePlayer, bytes,
		storagePoolID, storageMountUUID, deliverySource,
	)
}

func (s *Service) trackTraffic(
	userID, fileID, qualityID, audioID uint,
	source string,
	bytes uint64,
	storagePoolID uint,
	storageMountUUID, deliverySource string,
) {
	if bytes == 0 {
		return
	}
	if s.Deps.Traffic != nil {
		s.Deps.Traffic.Record(traffic.Event{
			UserID: userID, FileID: fileID, QualityID: qualityID, AudioID: audioID,
			Source: source, Bytes: bytes, StoragePoolID: storagePoolID,
			StorageMountUUID: storageMountUUID, DeliverySource: deliverySource,
		})
		return
	}
	now := time.Now().UTC()
	if err := s.Deps.DB.Create(&models.TrafficLog{
		Model:            models.Model{CreatedAt: &now, UpdatedAt: &now},
		UserID:           userID,
		FileID:           fileID,
		QualityID:        qualityID,
		AudioID:          audioID,
		Source:           source,
		Bytes:            bytes,
		RequestCount:     1,
		BucketStart:      now.Truncate(time.Minute).Unix(),
		StoragePoolID:    storagePoolID,
		StorageMountUUID: storageMountUUID,
		DeliverySource:   deliverySource,
	}).Error; err != nil {
		log.Printf("component=traffic_recorder event=direct_write_failed error=%q", err)
	}
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
