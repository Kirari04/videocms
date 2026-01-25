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
