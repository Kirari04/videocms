package models

type RemoteDownloadLog struct {
	Model
	RemoteDownloadID uint   `gorm:"uniqueIndex:idx_remote_download_log_source,where:remote_download_id > 0" json:"-"`
	UserID           uint   `gorm:"index"`
	Domain           string `gorm:"index;size:255;"`
	Bytes            uint64
	Seconds          float64
}

type RemoteDownloadStatsGetValidation struct {
	From   string `query:"from"`
	To     string `query:"to"`
	Points int    `query:"points"`
	UserID uint   `query:"user_id"`
}
