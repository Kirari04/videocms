package models

import "time"

const (
	DownloadJobStatusQueued    = "queued"
	DownloadJobStatusPreparing = "preparing"
	DownloadJobStatusReady     = "ready"
	DownloadJobStatusFailed    = "failed"
	DownloadJobStatusCanceled  = "canceled"
	DownloadJobStatusExpired   = "expired"
)

type DownloadJob struct {
	Model
	BackgroundJobID string `gorm:"size:36;index" json:"backgroundJobId,omitempty"`
	UUID            string `gorm:"size:36;uniqueIndex"`
	LinkID          uint   `gorm:"index"`
	LinkUUID        string `gorm:"size:64;index"`
	FileID          uint   `gorm:"index"`
	UserID          uint   `gorm:"index"`
	QualityID       uint   `gorm:"index"`
	QualityName     string `gorm:"size:20"`
	Container       string `gorm:"size:8"`
	AudioUUIDs      string `gorm:"type:text"`
	SubtitleUUIDs   string `gorm:"type:text"`
	SelectionHash   string `gorm:"size:64;index"`
	MediaDuration   float64

	Status       string `gorm:"size:24;index"`
	Progress     float64
	OutputPath   string `gorm:"size:1024" json:"-"`
	OutputName   string `gorm:"size:255"`
	OutputSize   int64
	ErrorCode    string `gorm:"size:64"`
	ErrorMessage string `gorm:"size:512"`

	StartedAt  *time.Time
	ReadyAt    *time.Time
	FinishedAt *time.Time
	ExpiresAt  *time.Time `gorm:"index"`
}

func IsDownloadJobTerminal(status string) bool {
	switch status {
	case DownloadJobStatusReady,
		DownloadJobStatusFailed,
		DownloadJobStatusCanceled,
		DownloadJobStatusExpired:
		return true
	default:
		return false
	}
}
