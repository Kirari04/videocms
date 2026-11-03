package models

import "time"

// Revocations are durable across restarts and apply to every refreshed token
// in the same session. IDs are never reused; legacy JWTs use a token digest.
type RevokedAuthSession struct {
	SessionID string    `gorm:"primaryKey;size:72" json:"-"`
	UserID    uint      `gorm:"index" json:"-"`
	RevokedAt time.Time `json:"-"`
}
