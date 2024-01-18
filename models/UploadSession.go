package models

import "github.com/golang-jwt/jwt/v5"

type UploadSession struct {
	Model
	Name           string `gorm:"size:128;"`
	UUID           string
	Hash           string `gorm:"size:128;" json:"-"`
	Size           int64
	ChunckCount    int
	SessionFolder  string  `gorm:"size:120;" json:"-"`
	ParentFolder   *Folder `json:"-"`
	ParentFolderID uint    `json:"-"`
	User           User    `json:"-"`
	UserID         uint
	UploadChuncks  []UploadChunck `json:"-"`
}

type UploadSessionClaims struct {
	UUID   string `json:"uuid"`
	UserID uint   `json:"userid"`
	jwt.RegisteredClaims
}

type UploadSessionValidation struct {
	Name           string `validate:"required,min=1,max=128" json:"Name" form:"Name"`
	Size           int64  `validate:"required,number,min=1" json:"Size" form:"Size"`
	ParentFolderID uint   `validate:"number" json:"ParentFolderID" form:"ParentFolderID"`
}

type DeleteUploadSessionValidation struct {
	UploadSessionUUID string `validate:"required,uuid_rfc4122" json:"UploadSessionUUID" form:"UploadSessionUUID"`
}
