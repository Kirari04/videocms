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
