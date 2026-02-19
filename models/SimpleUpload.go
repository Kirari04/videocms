package models

type SimpleUploadValidation struct {
	Name           string `validate:"required,min=1,max=128" form:"Name"`
	ParentFolderID uint   `validate:"number" form:"ParentFolderID"`
}
