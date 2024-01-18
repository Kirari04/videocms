package models

type WebPage struct {
	Model
	Path         string `gorm:"size:50;"`
	Title        string `gorm:"size:128;"`
	Html         string `gorm:"size:50000;"`
	ListInFooter bool
}

type WebPageCreateValidation struct {
	Path         string `validate:"required,dirpath,min=2,max=50"`
	Title        string `validate:"required,min=2,max=128"`
	Html         string `validate:"required,min=0,max=50000"`
	ListInFooter *bool  `validate:"required,boolean"`
}

type WebPageUpdateValidation struct {
	WebPageID    uint   `validate:"required,number"`
	Path         string `validate:"required,dirpath,min=2,max=50"`
	Title        string `validate:"required,min=2,max=128"`
	Html         string `validate:"required,min=0,max=50000"`
	ListInFooter *bool  `validate:"required,boolean"`
}

type WebPageDeleteValidation struct {
	WebPageID uint `validate:"required,number"`
}

type WebPageGetValidation struct {
	Path string `validate:"required,dirpath,min=2,max=50" query:"Path"`
}
