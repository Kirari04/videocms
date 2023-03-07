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
	Encoding      bool
	Progress      float64
	Failed        bool
	Ready         bool
	Error         string `json:"-"`
	File          File   `json:"-"`
	FileID        uint
}
