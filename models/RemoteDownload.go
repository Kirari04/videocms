package models

import (
	"time"
)

const (
	RemoteDownloadStatusPending     = "pending"
	RemoteDownloadStatusDownloading = "downloading"
	RemoteDownloadStatusImporting   = "importing"
	RemoteDownloadStatusCompleted   = "completed"
	RemoteDownloadStatusFailed      = "failed"
	RemoteDownloadStatusCanceling   = "canceling"
	RemoteDownloadStatusCanceled    = "canceled"
)

type RemoteDownload struct {
	Model
	UserID            uint    `gorm:"index"`
	ParentFolderID    uint    `gorm:"index"`
	Url               string  `gorm:"size:2048;"`
	Name              string  `gorm:"size:128;"`
	Status            string  `gorm:"size:32;"` // pending, downloading, importing, completed, failed, canceling, canceled
	Progress          float64 // 0.0 to 1.0
	Error             string  `gorm:"size:1024;"`
	TempPath          string  `gorm:"size:1024;" json:"-"`
	LinkID            uint    `gorm:"index"`
	LinkUUID          string  `gorm:"size:64;"`
	FileID            uint    `gorm:"index"`
	BytesDownloaded   int64
	TotalSize         int64
	Duration          float64
	StartedAt         *time.Time
	FinishedAt        *time.Time
	CancelRequestedAt *time.Time
	CanceledAt        *time.Time
}

type RemoteDownloadRequest struct {
	Urls           []string `json:"urls" validate:"required,min=1,max=20,dive,url"`
	ParentFolderID uint     `json:"parentFolderID"`
}

type RemoteDownloadClearRequest struct {
	Statuses []string `json:"statuses" validate:"required,min=1,max=3"`
}

func IsRemoteDownloadTerminal(status string) bool {
	switch status {
	case RemoteDownloadStatusCompleted, RemoteDownloadStatusFailed, RemoteDownloadStatusCanceled:
		return true
	default:
		return false
	}
}

func IsRemoteDownloadActive(status string) bool {
	switch status {
	case RemoteDownloadStatusPending, RemoteDownloadStatusDownloading, RemoteDownloadStatusImporting, RemoteDownloadStatusCanceling:
		return true
	default:
		return false
	}
}

func ActiveRemoteDownloadStatuses() []string {
	return []string{
		RemoteDownloadStatusPending,
		RemoteDownloadStatusDownloading,
		RemoteDownloadStatusImporting,
		RemoteDownloadStatusCanceling,
	}
}
