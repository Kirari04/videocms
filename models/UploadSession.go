package models

import "time"

const (
	UploadProtocolTus    = "tus"
	UploadProtocolSimple = "simple"

	UploadKindSingle  = "single"
	UploadKindPartial = "partial"
	UploadKindFinal   = "final"

	UploadStatusCreated   = "created"
	UploadStatusUploading = "uploading"
	UploadStatusUploaded  = "uploaded"
	UploadStatusImporting = "importing"
	UploadStatusDone      = "done"
	UploadStatusFailed    = "failed"
	UploadStatusCanceled  = "canceled"
	UploadStatusExpired   = "expired"
)

type UploadSession struct {
	Model
	UUID             string
	ClientUploadUUID string
	TusID            string `gorm:"index"`
	Protocol         string `gorm:"size:16;index"`
	Kind             string `gorm:"size:16;index"`
	Status           string `gorm:"size:16;index"`
	Name             string `gorm:"size:128;"`
	Hash             string `gorm:"size:128;" json:"-"`
	Size             int64
	Offset           int64
	QuotaBytes       int64
	PartCount        int
	StoragePath      string `gorm:"size:255;" json:"-"`
	InfoPath         string `gorm:"size:255;" json:"-"`
	ParentFolder     *Folder
	ParentFolderID   uint
	User             User `json:"-"`
	UserID           uint
	File             *File `json:"-"`
	FileID           uint
	Link             *Link `json:"-"`
	LinkID           uint
	ExpiresAt        *time.Time
	CompletedAt      *time.Time
	FinalizedAt      *time.Time
	Error            string       `gorm:"size:1000;"`
	UploadParts      []UploadPart `gorm:"foreignKey:UploadSessionID" json:"-"`
}

type UploadPart struct {
	Model
	UploadSession          UploadSession `json:"-"`
	UploadSessionID        uint
	PartialUploadSession   UploadSession `json:"-"`
	PartialUploadSessionID uint
	Index                  int
	TusID                  string `gorm:"index"`
}

type UploadSessionsGetResponse struct {
	ID               uint       `json:"ID"`
	CreatedAt        *time.Time `json:"CreatedAt"`
	Name             string     `json:"Name"`
	UUID             string     `json:"UUID"`
	ClientUploadUUID string     `json:"ClientUploadUUID"`
	TusID            string     `json:"TusID"`
	Size             int64      `json:"Size"`
	Offset           int64      `json:"Offset"`
	PartCount        int        `json:"PartCount"`
	Status           string     `json:"Status"`
	ExpiresAt        *time.Time `json:"ExpiresAt"`
}
