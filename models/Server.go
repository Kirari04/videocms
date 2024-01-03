package models

import "time"

type Server struct {
	Model
	UUID     string
	Hostname string
	Token    string `gorm:"size:512;" json:"-"`
	LastPing time.Time
}

type ServerDeleteValidation struct {
	ServerID uint `validate:"required,number"`
}

type ServerCreateValidation struct {
	Hostname string `validate:"required,hostname"`
}
