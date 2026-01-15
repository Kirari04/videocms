package models

type SystemResource struct {
	Model
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
	From     string `query:"from"`
	To       string `query:"to"`
	Points   int    `query:"points"`
	Interval string `query:"interval"`
}
