package models

type User struct {
	Model
	Username string `gorm:"unique;size:32;"`
	Hash     string `gorm:"size:250;" json:"-"`
	Admin    bool
	Email    string
	Balance  float64
	Storage  int64
	Settings UserSettings

	Folders  []Folder  `json:"-"`
	Webhooks []Webhook `json:"-"`
}

type UserLoginValidation struct {
	Username string `validate:"required,min=3,max=32"`
	Password string `validate:"required,min=8,max=250"`
}

type UserRegisterValidation struct {
	Username string `validate:"required,min=3,max=32"`
	Password string `validate:"required,min=8,max=250"`
	Admin    *bool  `validate:"required,boolean"`
}
