package models

import (
	"gorm.io/gorm"
)

type Folder struct {
	gorm.Model
	Name           string `gorm:"size:120;"`
	User           User   `json:"-"`
	UserID         uint
	ParentFolder   *Folder `json:"-"`
	ParentFolderID uint
}

type FolderCreateValidation struct {
	Name           string `validate:"required,min=1,max=120"`
	ParentFolderID uint   `validate:"number"`
}

type FolderListValidation struct {
	ParentFolderID uint `validate:"number"`
}

type FolderDeleteValidation struct {
	FolderID uint `validate:"required,number"`
}
