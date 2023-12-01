package models

type UploadFileValidation struct {
	SessionJwtToken string `validate:"required,min=1"`
}
