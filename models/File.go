package models

import (
	"gorm.io/gorm"
)

type File struct {
	gorm.Model
	Name           string `gorm:"size:120;"`
	Size           int64
	Duration       float64
	Height         int64
	Width          int64
	Path           string `gorm:"size:120;"`
	User           User   `json:"-"`
	UserID         uint
	ParentFolder   *Folder `json:"-"`
	ParentFolderID uint
}

type FileCreateValidation struct {
	Name           string `validate:"required,min=1,max=120"`
	ParentFolderID uint   `validate:"number"`
}

type FileListValidation struct {
	ParentFolderID uint `validate:"number"`
}

type FileDeleteValidation struct {
	FileID uint `validate:"required,number"`
}