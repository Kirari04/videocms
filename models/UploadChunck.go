package models

type UploadChunck struct {
	Model
	Index           uint
	Path            string `gorm:"size:120;" json:"-"`
	UploadSession   UploadSession
	UploadSessionID uint
}

type UploadChunckValidation struct {
	Index           *uint  `validate:"required,min=0,max=10000"`
	SessionJwtToken string `validate:"required,jwt"`
}
