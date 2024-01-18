package models

type UploadFileValidation struct {
	SessionJwtToken string `validate:"required,min=1" json:"SessionJwtToken" form:"SessionJwtToken"`
}
