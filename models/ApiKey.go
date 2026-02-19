package models

import (
	"time"
)

type ApiKey struct {
	Model
	UserID     uint
	User       User       `json:"-"`
	Name       string     `json:"name"`
	Key        string     `gorm:"uniqueIndex" json:"-"`
	Prefix     string     `gorm:"size:8" json:"prefix"` // Just first 8 chars for identification
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

type CreateApiKeyValidation struct {
	Name      string     `validate:"required,min=3,max=32" json:"name" form:"name"`
	ExpiresAt *time.Time `json:"expires_at" form:"expires_at"`
}

type CreateApiKeyResponse struct {
	ID        uint       `json:"id"`
	Name      string     `json:"name"`
	Key       string     `json:"key"` // Shown only on creation
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type ApiKeyAuditLog struct {
	Model
	ApiKeyID uint   `gorm:"index"`
	UserID   uint   `gorm:"index"`
	Method   string `gorm:"size:8"`
	Path     string `gorm:"size:255"`
	IP       string `gorm:"size:45"`
}
