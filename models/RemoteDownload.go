package models

import (
	"time"
)

type RemoteDownload struct {
	Model
	UserID          uint    `gorm:"index"`
	ParentFolderID  uint    `gorm:"index"`
	Url             string  `gorm:"size:2048;"`
	Status          string  `gorm:"size:32;"` // pending, downloading, completed, failed
	Progress        float64 // 0.0 to 1.0
	Error           string  `gorm:"size:1024;"`
	FileID          uint    `gorm:"index"`
	BytesDownloaded int64
	TotalSize       int64
	Duration        float64
	StartedAt       *time.Time
	FinishedAt      *time.Time
}

type RemoteDownloadRequest struct {
	Urls           []string `json:"urls" validate:"required,min=1,max=20,dive,url"`
	ParentFolderID uint     `json:"parentFolderID"`
}
