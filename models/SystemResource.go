package models

type SystemResource struct {
	Model
	ServerID         *uint `gorm:"index" json:"-"`
	Server           *Server
	Cpu              float64
	Mem              float64
	NetOut           uint64
	NetIn            uint64
	DiskW            uint64
	DiskR            uint64
	ENCQualityQueue  int64
	ENCAudioQueue    int64
	ENCSubtitleQueue int64
}

type SystemResourceGetValidation struct {
	Interval string `query:"interval" validate:"required,oneof=5min 1h 7h"`
}
