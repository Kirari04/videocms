package models

type Tag struct {
	Model
	Name   string `gorm:"size:128;"`
	Links  []Link `gorm:"many2many:links_tags;"`
	UserId uint   `gorm:"index"`
	User   User
}

type TagCreateValidation struct {
	Name   string `validate:"required,min=1,max=120"`
	FileId uint   `validate:"required,number"`
}
