package models

type Folder struct {
	Model
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
	ParentFolderID uint `validate:"number" query:"ParentFolderID"`
}

type FolderDeleteValidation struct {
	FolderID uint `validate:"required,number"`
}
type FoldersDeleteValidation struct {
	FolderIDs []FolderDeleteValidation `validate:"required"`
}

type FolderUpdateValidation struct {
	Name           string `validate:"required,min=1,max=120"`
	FolderID       uint   `validate:"required,number"`
	ParentFolderID uint   `validate:"number"`
}
