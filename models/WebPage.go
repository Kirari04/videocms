package models

const (
	WebPageFormatMarkdown = "markdown"
	WebPageFormatHTML     = "html"
)

type WebPage struct {
	Model
	Path         string `gorm:"size:50;"`
	Title        string `gorm:"size:128;"`
	Content      string `gorm:"size:50000;"`
	Format       string `gorm:"size:16;not null;"`
	Published    bool   `gorm:"not null;"`
	ListInFooter bool
}

type WebPageCreateValidation struct {
	Path         string `validate:"required,min=2,max=50"`
	Title        string `validate:"required,min=2,max=128"`
	Content      string `validate:"required,max=50000"`
	Format       string `validate:"required,oneof=markdown html"`
	Published    *bool  `validate:"required,boolean"`
	ListInFooter *bool  `validate:"required,boolean"`
}

type WebPageUpdateValidation struct {
	WebPageID    uint   `validate:"required,number"`
	Path         string `validate:"required,min=2,max=50"`
	Title        string `validate:"required,min=2,max=128"`
	Content      string `validate:"required,max=50000"`
	Format       string `validate:"required,oneof=markdown html"`
	Published    *bool  `validate:"required,boolean"`
	ListInFooter *bool  `validate:"required,boolean"`
}

type WebPagePreviewValidation struct {
	Content string `validate:"required,max=50000"`
	Format  string `validate:"required,oneof=markdown html"`
}

type WebPageDeleteValidation struct {
	WebPageID uint `validate:"required,number"`
}

type WebPageGetValidation struct {
	Path string `validate:"required,min=2,max=50" query:"Path"`
}
