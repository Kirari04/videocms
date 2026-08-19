package models

import "time"

const (
	StorageProviderLocal = "local"
	StorageProviderS3    = "s3"
	StorageProviderSFTP  = "sftp"

	StorageMountLocalUUID = "local"
	StoragePoolLocalUUID  = "local"

	FileStorageAvailable   = "available"
	FileStorageUnavailable = "unavailable"

	StoragePoolMountPrimary = "primary"
	StoragePoolMountCache   = "cache"
)

// StorageMount is a durable identity for a configured storage backend. A
// detached mount remains available for reconnection until an administrator
// explicitly deletes its saved configuration.
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
	StoragePoolID    uint   `gorm:"primaryKey"`
	StorageMountID   uint   `gorm:"primaryKey"`
	Role             string `gorm:"size:16;index;default:primary"`
	CacheMaxBytes    int64
	CacheLastError   string `gorm:"size:1000"`
	CacheLastErrorAt *time.Time
	CreatedAt        *time.Time

	StoragePool  StoragePool  `gorm:"foreignKey:StoragePoolID" json:"-"`
	StorageMount StorageMount `gorm:"foreignKey:StorageMountID" json:"-"`
}

// StorageCacheEntry records a disposable, verified playback copy. Files keep
// their authoritative StorageID; deleting this row or its object must never
// affect media ownership or availability.
type StorageCacheEntry struct {
	Model
	StoragePoolID    uint   `gorm:"index;uniqueIndex:idx_storage_cache_origin"`
	OriginMountID    string `gorm:"size:64;index;uniqueIndex:idx_storage_cache_origin"`
	ObjectKeyHash    string `gorm:"size:64;uniqueIndex:idx_storage_cache_origin"`
	ObjectKey        string `gorm:"type:text"`
	CacheMountID     uint   `gorm:"index"`
	CacheObjectKey   string `gorm:"type:text"`
	FileID           uint   `gorm:"index"`
	FileCacheVersion uint64
	Size             int64
	SourceETag       string `gorm:"size:255"`
	ContentType      string `gorm:"size:255"`
	CacheControl     string `gorm:"size:255"`
	SourceModTime    *time.Time
	LastAccessedAt   time.Time `gorm:"index"`
}
