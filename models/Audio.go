package models

import "gorm.io/gorm"

type Audio struct {
	Model
	UUID          string
	Name          string `gorm:"size:120;"`
	Lang          string `gorm:"size:10;"`
	Path          string `gorm:"size:120;" json:"-"`
	OriginalCodec string `json:"-"`
	Index         int
	Codec         string
	Type          string
	OutputFile    string `json:"-"`
	Encoding      bool
	Progress      float64
	Failed        bool
	Ready         bool   `json:"-"`
	Error         string `json:"-"`
	File          File   `json:"-"`
	FileID        uint
}

func (c *Audio) SetProcess(v float64) {
	c.Progress = v
}
func (c *Audio) GetProcess() float64 {
	return c.Progress
}
func (c *Audio) Save(DB *gorm.DB) *gorm.DB {
	return DB.Save(c)
}

type AudioGetValidation struct {
	UUID      string `validate:"required,uuid_rfc4122"`
	AUDIOUUID string `validate:"required,uuid_rfc4122"`
	FILE      string `validate:"required"`
}

type AvailableAudio struct {
	Type       string // hls | ogg | mp3
	Codec      string
	OutputFile string
}

var AvailableAudios = []AvailableAudio{
	{
		Type:       "hls",
		Codec:      "aac",
		OutputFile: "audio.m3u8",
	},
	// {
	// 	Type:       "ogg",
	// 	Codec:      "libopus",
	// 	OutputFile: "audio.ogg",
	// },
	// {
	// 	Type:       "mp3",
	// 	Codec:      "libmp3lame",
	// 	OutputFile: "audio.mp3",
	// },
}
