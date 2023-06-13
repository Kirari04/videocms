package models

type UploadChunck struct {
	Model
	Index           uint
	Path            string `gorm:"size:120;" json:"-"`
	UploadSession   UploadSession
	UploadSessionID uint
}

type UploadChunckValidation struct {
	Index           string `validate:"required,min=1,max=128"`
	SessionJwtToken string `validate:"required,min=1"`
}
