package inits

import (
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func MigrateModels(gormDB *gorm.DB) error {
	if gormDB == nil {
		return errors.New("DB is nil while attempting to migrate")
	}
	if err := gormDB.AutoMigrate(
		&models.User{},
		&models.Folder{},
		&models.File{},
		&models.Link{},
		&models.Quality{},
		&models.Subtitle{},
		&models.Audio{},
		&models.UploadSession{},
		&models.UploadPart{},
		&models.Webhook{},
		&models.WebPage{},
		&models.Setting{},
		&models.ApiKey{},
		&models.ApiKeyAuditLog{},
		&models.SystemResource{},
		&models.Tag{},
		&models.TagLinks{},
		&models.TrafficLog{},
		&models.UploadLog{},
		&models.EncodingLog{},
		&models.RemoteDownload{},
		&models.RemoteDownloadLog{},
	); err != nil {
		return err
	}

	// init default admin user
	// if no user exists
	var count int64
	if err := gormDB.Model(&models.User{}).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to count users: %w", err)
	}
	if count == 0 {
		rawhash, _ := bcrypt.GenerateFromPassword([]byte("12345678"), 14)
		user := models.User{
			Username: "admin",
			Hash:     string(rawhash),
			Admin:    true,
			Settings: models.UserSettings{
				WebhooksEnabled:       true,
				WebhooksMax:           100,
				MaxRemoteDownloads:    models.DefaultMaxRemoteDownloads,
				RemoteDownloadEnabled: boolPtr(true),
			},
		}
		if res := gormDB.Create(&user); res.Error != nil {
			return fmt.Errorf("error while creating admin user: %w", res.Error)
		}
	}
	return nil
}

func boolPtr(value bool) *bool {
	return &value
}
