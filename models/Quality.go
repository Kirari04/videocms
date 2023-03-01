package models

import (
	"gorm.io/gorm"
)

type Quality struct {
	gorm.Model
	Name         string `gorm:"size:20;"`
	Height       int64
	Width        int64
	Crf          int `json:"-"`
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
}

var AvailableQualitys = []AvailableQuality{
	{
		Name:       "240p",
		FolderName: "240p",
		Height:     240,
		Width:      426,
		Crf:        30,
	},
	{
		Name:       "360p",
		FolderName: "360p",
		Height:     360,
		Width:      640,
		Crf:        26,
	},
	{
		Name:       "480p",
		FolderName: "480p",
		Height:     480,
		Width:      854,
		Crf:        26,
	},
	{
		Name:       "720p",
		FolderName: "720p",
		Height:     720,
		Width:      1280,
		Crf:        26,
	},
	{
		Name:       "1080p",
		FolderName: "1080p",
		Height:     1080,
		Width:      1920,
		Crf:        24,
	},
	{
		Name:       "1440p",
		FolderName: "1440p",
		Height:     1440,
		Width:      2560,
		Crf:        24,
	},
	{
		Name:       "2160p",
		FolderName: "2160p",
		Height:     2160,
		Width:      3840,
		Crf:        24,
	},
	// removed 8k from list
	// {
	// 	Name:       "4320p",
	// 	FolderName: "4320p",
	// 	Height:     4320,
	// 	Width:      7680,
	// 	Crf:        24,
	// },
}
