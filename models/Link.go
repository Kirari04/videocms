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
	Tags           []Tag `gorm:"many2many:links_tags;"`
}

type LinkListValidation struct {
	ParentFolderID uint `validate:"number" query:"ParentFolderID"`
}

type LinkDeleteValidation struct {
	LinkID uint `validate:"required,number" form:"LinkID"`
}

type LinkUpdateValidation struct {
	LinkID         uint   `validate:"required,number" form:"LinkID"`
	Name           string `validate:"required,min=1,max=120" form:"Name"`
	ParentFolderID uint   `validate:"number" form:"ParentFolderID"`
}

type LinksDeleteValidation struct {
	LinkIDs []LinkDeleteValidation `validate:"required"`
}

type LinkGetValidation struct {
	LinkID uint `validate:"required,number" query:"LinkID"`
}
