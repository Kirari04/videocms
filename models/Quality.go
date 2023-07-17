package models

type Quality struct {
	Model
	Name         string `gorm:"size:20;"`
	Height       int64
	Width        int64
	Size         int64
	Crf          int `json:"-"`
	Type         string
	Muted        bool
	AudioCodec   string `json:"-"`
	AvgFrameRate float64
	Path         string `gorm:"size:120;" json:"-"`
	OutputFile   string
	Encoding     bool
	Progress     float64
	Failed       bool
	Ready        bool   `json:"-"`
	Error        string `json:"-"`
	File         File   `json:"-"`
	FileID       uint
}

type AvailableQuality struct {
	Name       string
	FolderName string
	Height     int64
	Width      int64
	Crf        int
	Type       string // hls | vp9 | av1
	Muted      bool
	AudioCodec string
	OutputFile string
	Enabled    bool
}

var AvailableQualitys []AvailableQuality
