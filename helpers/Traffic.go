package helpers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
)

func TrackTraffic(userID, fileID, qualityID, audioID uint, bytes uint64) {
	if bytes == 0 {
		return
	}
	traffic := models.TrafficLog{
		UserID:    userID,
		FileID:    fileID,
		QualityID: qualityID,
		AudioID:   audioID,
		Bytes:     bytes,
	}
	inits.DB.Create(&traffic)
}

func TrackUpload(userID uint, bytes uint64) {
	if bytes == 0 {
		return
	}
	upload := models.UploadLog{
		UserID: userID,
		Bytes:  bytes,
	}
	inits.DB.Create(&upload)
}

func TrackEncoding(userID uint, fileID uint, taskType string, seconds float64) {
	if seconds <= 0 {
		return
	}
	log := models.EncodingLog{
		UserID:  userID,
		FileID:  fileID,
		Type:    taskType,
		Seconds: seconds,
	}
	inits.DB.Create(&log)
}
