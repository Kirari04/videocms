package models

import (
	"gorm.io/gorm"
)

type Subtitle struct {
	gorm.Model
	UUID     string
	Name     string `gorm:"size:120;"`
	Lang     string `gorm:"size:10;"`
	Path     string `gorm:"size:120;" json:"-"`
	Index    int
	Encoding bool
	Progress float64
	Failed   bool
	Ready    bool   `json:"-"`
	Error    string `json:"-"`
	File     File
	FileID   uint
}