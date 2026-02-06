package models

type TrafficLog struct {
	Model
	UserID    uint `gorm:"index"`
	FileID    uint `gorm:"index"`
	QualityID uint `gorm:"index"`
	AudioID   uint `gorm:"index"`
	Bytes     uint64
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
