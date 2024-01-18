package models

type UploadChunck struct {
	Model
	Index           uint
	Path            string `gorm:"size:120;" json:"-"`
	UploadSession   UploadSession
	UploadSessionID uint
}

type UploadChunckValidation struct {
	Index           *uint  `validate:"required,min=0,max=10000" json:"Index" form:"Index"`
	SessionJwtToken string `validate:"required,jwt" json:"SessionJwtToken" form:"SessionJwtToken"`
}
