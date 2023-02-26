package models

import (
	"gorm.io/gorm"
)

type Link struct {
	gorm.Model
	UUID           string
	File           File `json:"-"`
	FileID         uint `json:"-"`
	User           User `json:"-"`
	UserID         uint
	ParentFolder   *Folder `json:"-"`
	ParentFolderID uint
}
