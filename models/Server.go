package models

import "time"

type Server struct {
	Model
	UUID     string
	Hostname string `gorm:"size:128;"`
	Token    string `gorm:"size:512;" json:"-"`
	LastPing time.Time
}

type ServerDeleteValidation struct {
	ServerID uint `validate:"required,number,gte=1"`
}

type ServerCreateValidation struct {
	Hostname string `validate:"required,hostname,max=128"`
}
