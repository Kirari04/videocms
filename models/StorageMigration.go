package models

import "time"

const (
	StorageMigrationQueued             = "queued"
	StorageMigrationRunning            = "running"
	StorageMigrationPaused             = "paused"
	StorageMigrationFailed             = "failed"
	StorageMigrationCanceled           = "canceled"
	StorageMigrationRetainingOriginals = "retaining_originals"
	StorageMigrationCleaningOriginals  = "cleaning_originals"
	StorageMigrationCompleted          = "completed"
	StorageMigrationOriginalsRetained  = "originals_retained"

	StorageMigrationItemPending        = "pending"
	StorageMigrationItemCopying        = "copying"
	StorageMigrationItemVerifying      = "verifying"
	StorageMigrationItemCutover        = "cutover"
	StorageMigrationItemCleanupPending = "cleanup_pending"
	StorageMigrationItemCleaning       = "cleaning"
	StorageMigrationItemCleaned        = "cleaned"
	StorageMigrationItemOriginalKept   = "original_kept"
	StorageMigrationItemFailed         = "failed"
	StorageMigrationItemCanceled       = "canceled"
)

type StorageMigration struct {
	Model
	UUID                string `gorm:"uniqueIndex;size:64"`
	SourcePoolID        uint   `gorm:"index"`
	DestinationPoolID   uint   `gorm:"index"`
	SourcePoolName      string `gorm:"size:120"`
	DestinationPoolName string `gorm:"size:120"`
	BackgroundJobID     string `gorm:"size:36;index"`
	CleanupJobID        string `gorm:"size:36;index"`
	Status              string `gorm:"size:32;index"`
	Phase               string `gorm:"size:120"`
	FileCount           int64
	PlannedBytes        int64
	ActualBytes         int64
	CopiedBytes         int64
	CutoverCount        int64
	CleanedCount        int64
	CreatedByID         uint   `gorm:"index"`
	CreatedByName       string `gorm:"size:120"`
	CancelGeneration    int
	KeepOriginals       bool
	CleanupAfter        *time.Time `gorm:"index"`
	StartedAt           *time.Time
	CopyCompletedAt     *time.Time
	CompletedAt         *time.Time
	CanceledAt          *time.Time
	ErrorCode           string `gorm:"size:80"`
	ErrorMessage        string `gorm:"size:512"`

	Items []StorageMigrationItem `gorm:"foreignKey:MigrationID" json:"-"`
}

type StorageMigrationItem struct {
	Model
	MigrationID        uint   `gorm:"index;uniqueIndex:idx_storage_migration_file"`
	FileID             uint   `gorm:"index;uniqueIndex:idx_storage_migration_file"`
	FileUUID           string `gorm:"size:64;index"`
	SourceMountID      string `gorm:"size:64;index"`
	DestinationMountID string `gorm:"size:64;index"`
	Status             string `gorm:"size:32;index"`
	ReservationKey     string `gorm:"size:80;uniqueIndex:idx_storage_migration_reservation,where:reservation_key <> ''" json:"-"`
	DestinationOwned   bool
	PlannedBytes       int64
	BytesTotal         int64
	BytesCopied        int64
	ObjectCount        int
	ObjectsVerified    int
	ProgressMessage    string `gorm:"size:255"`
	ErrorCode          string `gorm:"size:80"`
	ErrorMessage       string `gorm:"size:512"`
	CopyStartedAt      *time.Time
	VerifiedAt         *time.Time
	CutoverAt          *time.Time
	CleanedAt          *time.Time
}
