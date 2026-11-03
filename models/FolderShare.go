package models

import "time"

// FolderShare grants a visitor access to one owner's folder subtree. Each link
// has independent permissions and can be revoked without changing other links.
type FolderShare struct {
	Model
	Token            string `gorm:"size:43;uniqueIndex"`
	UserID           uint   `gorm:"index" json:"-"`
	FolderID         uint   `gorm:"index"`
	Name             string `gorm:"size:120"`
	AllowRead        bool
	AllowUpload      bool
	MaxVideoSize     int64 `gorm:"not null;default:0"`
	MaxFolderSize    int64 `gorm:"not null;default:0"`
	ReturnUploadLink *bool `gorm:"not null;default:true"`
	RequireCaptcha   bool
	PasswordHash     string `gorm:"size:72" json:"-"`
	AccessVersion    string `gorm:"size:36" json:"-"`
	ExpiresAt        *time.Time
}

func (share *FolderShare) ReturnsUploadLink() bool {
	return share.ReturnUploadLink == nil || *share.ReturnUploadLink
}

type FolderShareWrite struct {
	FolderID         uint   `json:"FolderID" validate:"required"`
	Name             string `json:"Name" validate:"max=120"`
	AllowRead        bool   `json:"AllowRead"`
	AllowUpload      bool   `json:"AllowUpload"`
	MaxVideoSize     *int64 `json:"MaxVideoSize" validate:"omitempty,gte=0"`
	MaxFolderSize    *int64 `json:"MaxFolderSize" validate:"omitempty,gte=0"`
	ReturnUploadLink *bool  `json:"ReturnUploadLink"`
	RequireCaptcha   bool   `json:"RequireCaptcha"`
	// Omitted preserves an existing password; empty removes protection.
	Password  *string    `json:"Password" validate:"omitempty,max=72"`
	ExpiresAt *time.Time `json:"ExpiresAt"`
}
