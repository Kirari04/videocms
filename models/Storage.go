package models

import "time"

const (
	StorageProviderLocal = "local"
	StorageProviderS3    = "s3"

	StorageMountLocalUUID = "local"
	StoragePoolLocalUUID  = "local"

	FileStorageAvailable   = "available"
	FileStorageUnavailable = "unavailable"
)

// StorageMount is a durable identity for a configured storage backend. A
// mount record remains as a tombstone when it is unmounted so files can retain
// their original storage ID and be reconnected later.
type StorageMount struct {
	Model
	UUID                 string `gorm:"uniqueIndex;size:64"`
	Name                 string `gorm:"size:120"`
	Provider             string `gorm:"size:32;index"`
	Configuration        string `gorm:"type:text" json:"-"`
	EncryptedCredentials string `gorm:"type:text" json:"-"`
	Mounted              bool   `gorm:"index"`
	System               bool
	LastError            string `gorm:"size:1000"`
	LastCheckedAt        *time.Time
	UnmountedAt          *time.Time
}

type StoragePool struct {
	Model
	UUID      string `gorm:"uniqueIndex;size:64"`
	Name      string `gorm:"uniqueIndex;size:120"`
	IsDefault bool   `gorm:"index"`
	System    bool
	Members   []StoragePoolMount `gorm:"foreignKey:StoragePoolID" json:"-"`
}

type StoragePoolMount struct {
	StoragePoolID  uint `gorm:"primaryKey"`
	StorageMountID uint `gorm:"primaryKey"`
	CreatedAt      *time.Time

	StoragePool  StoragePool  `gorm:"foreignKey:StoragePoolID" json:"-"`
	StorageMount StorageMount `gorm:"foreignKey:StorageMountID" json:"-"`
}
