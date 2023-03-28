package models

import (
	"gorm.io/gorm"
)

type Subtitle struct {
	gorm.Model
	UUID          string
	Name          string `gorm:"size:120;"`
	Lang          string `gorm:"size:10;"`
	Path          string `gorm:"size:120;" json:"-"`
	OriginalCodec string `json:"-"`
	Index         int    `json:"-"`
	Codec         string
	Type          string
	OutputFile    string
	Encoding      bool
	Progress      float64
	Failed        bool
	Ready         bool
	Error         string `json:"-"`
	File          File   `json:"-"`
	FileID        uint
}

type AvailableSubtitle struct {
	Type       string // ass | vtt
	Codec      string
	OutputFile string
}

var AvailableSubtitles = []AvailableSubtitle{
	{
		Type:       "ass",
		Codec:      "ass",
		OutputFile: "out.ass",
	},
	{
		Type:       "vtt",
		Codec:      "webvtt",
		OutputFile: "out.vtt",
	},
}
