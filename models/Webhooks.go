package models

type Webhook struct {
	Model
	Name     string `gorm:"size:128;"`
	Url      string `gorm:"size:255;"`
	Rpm      int
	ReqQuery string
	ResField string

	User   User `json:"-"`
	UserID uint
}

type WebhookListValidation struct {
}

type WebhookCreateValidation struct {
	Name     string `validate:"required,min=1,max=120"`
	Url      string `validate:"required,http_url,min=4,max=255"`
	Rpm      int    `validate:"required,number,min=1,max=60"`
	ReqQuery string `validate:"required,alpha,min=0,max=50"`
	ResField string `validate:"required,alpha,min=0,max=50"`
}

type WebhookUpdateValidation struct {
	WebhookID uint   `validate:"required,number"`
	Name      string `validate:"required,min=1,max=120"`
	Url       string `validate:"required,http_url,min=4,max=255"`
	Rpm       int    `validate:"required,number,min=1,max=60"`
	ReqQuery  string `validate:"required,alpha,min=0,max=50"`
	ResField  string `validate:"required,alpha,min=0,max=50"`
}

type WebhookDeleteValidation struct {
	WebhookID uint `validate:"required,number"`
}
