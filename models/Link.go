package models

type Link struct {
	Model
	UUID           string
	Name           string  `gorm:"size:128;"`
	File           File    `json:"-"`
	FileID         uint    `json:"-"`
	User           User    `json:"-"`
	UserID         uint    `json:"-"`
	ParentFolder   *Folder `json:"-"`
	ParentFolderID uint
}

type LinkListValidation struct {
	ParentFolderID uint `validate:"number"`
}

type LinkDeleteValidation struct {
	LinkID uint `validate:"required,number"`
}

type LinkUpdateValidation struct {
	LinkID         uint   `validate:"required,number"`
	Name           string `validate:"required,min=1,max=120"`
	ParentFolderID uint   `validate:"number"`
}

type LinksDeleteValidation struct {
	LinkIDs []LinkDeleteValidation `validate:"required"`
}

type LinkGetValidation struct {
	LinkID uint `validate:"required,number"`
}
