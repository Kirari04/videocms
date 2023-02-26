package models

import (
	"gorm.io/gorm"
)

type Folder struct {
	gorm.Model
	Name           string `gorm:"size:120;"`
	User           User
	UserID         uint
	ParentFolder   *Folder
	ParentFolderID *uint
}

type FolderCreateValidation struct {
	Name string `validate:"required,min=1,max=120"`
}

func (folder *FolderCreateValidation) ToFolder() Folder {
	return Folder{
		Name: folder.Name,
	}
}
