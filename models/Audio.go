package models

import (
	"gorm.io/gorm"
)

type Audio struct {
	gorm.Model
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

type AvailableAudio struct {
	Type       string // hls | opus
	Codec      string
	OutputFile string
}

var AvailableAudios = []AvailableAudio{
	{
		Type:       "hls",
		Codec:      "aac",
		OutputFile: "audio.m3u8",
	},
	{
		Type:       "opus",
		Codec:      "libopus",
		OutputFile: "audio.wav",
	},
}
