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
	Name           string `validate:"required,min=1,max=120" json:"Name" form:"Name"`
	ParentFolderID uint   `validate:"number" json:"ParentFolderID" form:"ParentFolderID"`
}

type FolderListValidation struct {
	ParentFolderID uint `validate:"number" query:"ParentFolderID"`
}

type FolderDeleteValidation struct {
	FolderID uint `validate:"required,number" json:"FolderID" form:"FolderID"`
}
type FoldersDeleteValidation struct {
	FolderIDs []FolderDeleteValidation `validate:"required" json:"FolderIDs" form:"FolderIDs"`
}

type FolderUpdateValidation struct {
	Name           string `validate:"required,min=1,max=120" json:"Name" form:"Name"`
	FolderID       uint   `validate:"required,number" json:"FolderID" form:"FolderID"`
	ParentFolderID uint   `validate:"number" json:"ParentFolderID" form:"ParentFolderID"`
}
