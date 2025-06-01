package models

import (
	"gorm.io/gorm"
)

type Quality struct {
	Model
	Name   string `gorm:"size:20;"`
	Height int64
	Width  int64
	Size   int64
	// Crf          int `json:"-"`

	VideoBitrate   string // example: 8000k
	AudioBitrate   string // example: 128k
	Profile        string // example: high
	Level          string // example: 5.2 | 5.1 | 4.2 | 4.0 | 3.1 | 3.0
	CodecStringAVC string // example: avc1.640034

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

func (c *Quality) SetProcess(v float64) {
	c.Progress = v
}
func (c *Quality) GetProcess() float64 {
	return c.Progress
}
func (c *Quality) Save(DB *gorm.DB) *gorm.DB {
	return DB.Save(c)
}

type AvailableQuality struct {
	Name       string
	FolderName string
	Height     int64
	Width      int64
	// Crf        int
	VideoBitrate   string // example: 8000k
	AudioBitrate   string // example: 128k
	Profile        string // example: high
	Level          string // example: 5.2| 5.1 | 4.2 | 4.0 | 3.1 | 3.0
	CodecStringAVC string // example: avc1.640034

	Type       string // hls
	Muted      bool
	OutputFile string
	Enabled    bool
}

var AvailableQualitys []AvailableQuality
