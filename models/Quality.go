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
	Type         string
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
	OutputFile string
}

var AvailableQualitys = []AvailableQuality{
	{
		Name:       "240p",
		FolderName: "240p",
		Height:     240,
		Width:      426,
		Crf:        30,
		Type:       "hls",
		OutputFile: "out.m3u8",
	},
	{
		Name:       "360p",
		FolderName: "360p",
		Height:     360,
		Width:      640,
		Crf:        26,
		Type:       "hls",
		OutputFile: "out.m3u8",
	},
	{
		Name:       "480p",
		FolderName: "480p",
		Height:     480,
		Width:      854,
		Crf:        26,
		Type:       "hls",
		OutputFile: "out.m3u8",
	},
	{
		Name:       "720p",
		FolderName: "720p",
		Height:     720,
		Width:      1280,
		Crf:        26,
		Type:       "hls",
		OutputFile: "out.m3u8",
	},
	{
		Name:       "1080p",
		FolderName: "1080p",
		Height:     1080,
		Width:      1920,
		Crf:        24,
		Type:       "hls",
		OutputFile: "out.m3u8",
	},
	{
		Name:       "1440p",
		FolderName: "1440p",
		Height:     1440,
		Width:      2560,
		Crf:        24,
		Type:       "hls",
		OutputFile: "out.m3u8",
	},
	{
		Name:       "2160p",
		FolderName: "2160p",
		Height:     2160,
		Width:      3840,
		Crf:        24,
		Type:       "hls",
		OutputFile: "out.m3u8",
	},
	// {
	// 	Name:       "480p",
	// 	FolderName: "480p",
	// 	Height:     480,
	// 	Width:      854,
	// 	Crf:        30,
	// 	Type:       "av1",
	// 	OutputFile: "out.mp4",
	// },
	{
		Name:       "480p",
		FolderName: "480p",
		Height:     480,
		Width:      854,
		Crf:        30,
		Type:       "vp9",
		OutputFile: "out.webm",
	},
}
