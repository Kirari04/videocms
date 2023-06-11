package models

type UploadSession struct {
	Model
	Name           string `gorm:"size:128;"`
	UUID           string
	Hash           string `gorm:"size:128;" json:"-"`
	Size           int64
	SessionFolder  string  `gorm:"size:120;" json:"-"`
	ParentFolder   *Folder `json:"-"`
	ParentFolderID uint
	User           User `json:"-"`
	UserID         uint
}

type UploadSessionValidation struct {
	Name           string `validate:"required,min=1,max=128"`
	Size           int64  `validate:"required,number,min=1"`
	Sha256         string `validate:"required,sha256"`
	ParentFolderID uint   `validate:"number"`
}
