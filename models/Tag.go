package models

import (
	"time"

	"gorm.io/gorm"
)

type Tag struct {
	Model
	Name   string  `gorm:"size:128;"`
	UserId uint    `gorm:"index" json:"-"`
	User   User    `json:"-"`
	Links  []*Link `gorm:"many2many:tag_links;" json:"-"`
}

type TagLinks struct {
	LinkID    uint `gorm:"primaryKey"`
	TagID     uint `gorm:"primaryKey"`
	CreatedAt time.Time
	DeletedAt gorm.DeletedAt
}

type TagCreateValidation struct {
	Name   string `validate:"required,min=1,max=120" json:"Name" form:"Name"`
	LinkId uint   `validate:"required,number" json:"LinkId" form:"LinkId"`
}

type TagDeleteValidation struct {
	TagID  uint `validate:"required,number" json:"TagID" form:"TagID"`
	LinkId uint `validate:"required,number" json:"LinkId" form:"LinkId"`
}
