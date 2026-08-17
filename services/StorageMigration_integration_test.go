package services

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestStorageMigrationS3ToSFTPIntegration(t *testing.T) {
	endpoint := os.Getenv("VIDEOCMS_S3_INTEGRATION_ENDPOINT")
	host := os.Getenv("VIDEOCMS_SFTP_INTEGRATION_HOST")
	if endpoint == "" || host == "" {
		t.Skip("S3 and SFTP integration services are not configured")
	}
	port, err := strconv.Atoi(os.Getenv("VIDEOCMS_SFTP_INTEGRATION_PORT"))
	if err != nil || port < 1 || port > 65535 {
		t.Fatalf("VIDEOCMS_SFTP_INTEGRATION_PORT is invalid")
	}
	fingerprints := strings.FieldsFunc(os.Getenv("VIDEOCMS_SFTP_INTEGRATION_HOST_KEY_FINGERPRINTS"), func(value rune) bool {
		return value == ',' || value == '\n' || value == '\r'
	})
	if len(fingerprints) == 0 {
		t.Fatal("VIDEOCMS_SFTP_INTEGRATION_HOST_KEY_FINGERPRINTS is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	runID := uuid.NewString()
	s3Store, err := storage.NewS3Store(ctx, storage.S3Options{
		Bucket: os.Getenv("VIDEOCMS_S3_INTEGRATION_BUCKET"), Region: os.Getenv("VIDEOCMS_S3_INTEGRATION_REGION"), Endpoint: endpoint,
		Prefix: "videocms-migration-integration/" + runID, AccessKeyID: os.Getenv("VIDEOCMS_S3_INTEGRATION_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("VIDEOCMS_S3_INTEGRATION_SECRET_ACCESS_KEY"), UsePathStyle: true,
		UploadPartSize: 5 * 1024 * 1024, UploadConcurrency: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	root := os.Getenv("VIDEOCMS_SFTP_INTEGRATION_ROOT")
	if root == "" {
		root = "/upload"
	}
	sftpStore, err := storage.NewSFTPStore(ctx, storage.SFTPOptions{
		Host: host, Port: port, Username: os.Getenv("VIDEOCMS_SFTP_INTEGRATION_USERNAME"), Root: root,
		Authentication: storage.SFTPAuthenticationPassword, HostKeyFingerprints: fingerprints,
		Password: os.Getenv("VIDEOCMS_SFTP_INTEGRATION_PASSWORD"),
	})
	if err != nil {
		_ = s3Store.Close()
		t.Fatal(err)
	}
	storageService, err := storage.NewService("s3", storage.LegacyMediaLayout{}, map[string]storage.Store{"s3": s3Store, "sftp": sftpStore})
	if err != nil {
		_ = s3Store.Close()
		_ = sftpStore.Close()
		t.Fatal(err)
	}

	fileUUID := uuid.NewString()
	prefix, err := storageService.Layout().FilePrefix(fileUUID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = storage.DeletePrefix(context.Background(), s3Store, prefix)
		_ = storage.DeletePrefix(context.Background(), sftpStore, prefix)
		_ = storageService.Close()
	})
	key := migrationTestKey(t, prefix.String()+"/source/original.mp4")
	putMigrationTestObject(t, s3Store, key, "garage-to-sftp-media")

	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.File{}, &models.Quality{}, &models.Audio{}, &models.Subtitle{}, &models.StorageMigration{}, &models.StorageMigrationAccount{}, &models.StorageMigrationItem{}, &models.StorageMigrationObject{}); err != nil {
		t.Fatal(err)
	}
	file := models.File{UUID: fileUUID, StorageID: "s3", StorageState: models.FileStorageAvailable, Size: int64(len("garage-to-sftp-media"))}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	migration := models.StorageMigration{UUID: runID, Status: models.StorageMigrationRunning, FileCount: 1, PlannedBytes: file.Size}
	if err := db.Create(&migration).Error; err != nil {
		t.Fatal(err)
	}
	item := models.StorageMigrationItem{
		MigrationID: migration.ID, FileID: file.ID, FileUUID: file.UUID, SourceMountID: "s3", DestinationMountID: "sftp",
		Status: models.StorageMigrationItemPending, ReservationKey: "file:" + strconv.FormatUint(uint64(file.ID), 10), PlannedBytes: file.Size,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	worker := NewWorkerGroup(&app.Deps{DB: db, Storage: storageService}, nil)
	if err := worker.migrateStorageItem(ctx, migration, &item); err != nil {
		t.Fatal(err)
	}
	assertMigrationTestObject(t, s3Store, key, "garage-to-sftp-media")
	assertMigrationTestObject(t, sftpStore, key, "garage-to-sftp-media")
	if err := db.First(&file, file.ID).Error; err != nil {
		t.Fatal(err)
	}
	if file.StorageID != "sftp" {
		t.Fatalf("file remained on %q after cutover", file.StorageID)
	}
	if err := worker.cleanupStorageMigrationItem(ctx, &item); err != nil {
		t.Fatal(err)
	}
	if _, err := s3Store.Stat(ctx, key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("S3 original remained after deferred cleanup: %v", err)
	}
	assertMigrationTestObject(t, sftpStore, key, "garage-to-sftp-media")
}
