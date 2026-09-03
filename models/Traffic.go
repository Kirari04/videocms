package models

import (
	"time"

	"gorm.io/gorm"
)

const (
	TrafficSourcePlayer   = "player"
	TrafficSourceDownload = "download"

	TrafficDeliverySourceOrigin = "origin"
	TrafficDeliverySourceCache  = "cache"
)

type TrafficLog struct {
	Model
	UserID       uint   `gorm:"index"`
	FileID       uint   `gorm:"index"`
	QualityID    uint   `gorm:"index"`
	AudioID      uint   `gorm:"index"`
	Source       string `gorm:"size:16;not null;default:player;index"`
	Bytes        uint64
	RequestCount uint64  `gorm:"not null;default:1"`
	BucketStart  int64   `gorm:"index"`
	RollupKey    *string `gorm:"size:64;uniqueIndex"`

	// Storage attribution is intentionally recorded only for responses that
	// read a configured storage mount. Older traffic and generated responses
	// keep these fields empty instead of being guessed during an upgrade.
	StoragePoolID    uint
	StorageMountUUID string `gorm:"size:64"`
	DeliverySource   string `gorm:"size:16"`
}

// BeforeCreate keeps directly-created and legacy-compatible traffic rows on
// the same indexed time axis as buffered rollups. RollupKey intentionally
// remains nil for individual rows so SQLite's unique index permits more than
// one legacy event in the same bucket.
func (t *TrafficLog) BeforeCreate(_ *gorm.DB) error {
	if t.RequestCount == 0 {
		t.RequestCount = 1
	}
	if t.BucketStart == 0 {
		at := time.Now().UTC()
		if t.CreatedAt != nil {
			at = t.CreatedAt.UTC()
		}
		t.BucketStart = at.Truncate(time.Minute).Unix()
	}
	return nil
}

type TrafficStatsGetValidation struct {
	From      string `query:"from"`
	To        string `query:"to"`
	Points    int    `query:"points"`
	Interval  string `query:"interval"`
	UserID    uint   `query:"user_id"`
	FileID    uint   `query:"file_id"`
	QualityID uint   `query:"quality_id"`
}

type UploadLog struct {
	Model
	UserID          uint `gorm:"index"`
	FileID          uint `gorm:"index"`
	UploadSessionID uint `gorm:"index"`
	Bytes           uint64
}

type UploadStatsGetValidation struct {
	From   string `query:"from"`
	To     string `query:"to"`
	Points int    `query:"points"`
	UserID uint   `query:"user_id"`
}

type EncodingLog struct {
	Model
	UserID  uint   `gorm:"index"`
	FileID  uint   `gorm:"index"`
	Type    string `gorm:"size:32"` // reconstruction, quality, audio, sub
	Seconds float64
}

type EncodingStatsGetValidation struct {
	From   string `query:"from"`
	To     string `query:"to"`
	Points int    `query:"points"`
	UserID uint   `query:"user_id"`
}
