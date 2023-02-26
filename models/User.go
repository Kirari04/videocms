package models

import (
	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	Username string `gorm:"unique;size:32;"`
	Hash     string `gorm:"size:250;"`
	Admin    bool
	Folders  []Folder
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

func (user *UserLoginValidation) ToUser() User {
	return User{
		Username: user.Username,
		Hash:     user.Username,
		Admin:    false,
	}
}
