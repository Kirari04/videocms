package models

import (
	"gorm.io/gorm"
)

type Link struct {
	gorm.Model
	UUID           string
	Name           string `gorm:"size:128;"`
	File           File   `json:"-"`
	FileID         uint   `json:"-"`
	User           User   `json:"-"`
	UserID         uint
	ParentFolder   *Folder `json:"-"`
	ParentFolderID uint
}

type LinkListValidation struct {
	ParentFolderID uint `validate:"number"`
}

type LinkDeleteValidation struct {
	LinkID uint `validate:"required,number"`
}
type LinksDeleteValidation struct {
	LinkIDs []LinkDeleteValidation `validate:"required"`
}

type LinkGetValidation struct {
	LinkID uint `validate:"required,number"`
}
