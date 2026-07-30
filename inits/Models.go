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
	hadWebPageTable := gormDB.Migrator().HasTable("web_pages")
	hadWebPageContent, err := hasWebPageColumn(gormDB, "content")
	if err != nil {
		return err
	}
	hadWebPageFormat, err := hasWebPageColumn(gormDB, "format")
	if err != nil {
		return err
	}
	hadWebPagePublished, err := hasWebPageColumn(gormDB, "published")
	if err != nil {
		return err
	}

	if hadWebPageTable && !hadWebPageContent {
		if err := gormDB.Exec("ALTER TABLE web_pages ADD COLUMN content text").Error; err != nil {
			return fmt.Errorf("failed to add webpage content column: %w", err)
		}
		if err := gormDB.Exec(
			"UPDATE web_pages SET content = html WHERE (content IS NULL OR content = '') AND html != ''",
		).Error; err != nil {
			return fmt.Errorf("failed to migrate webpage content: %w", err)
		}
	}
	if hadWebPageTable && !hadWebPageFormat {
		if err := gormDB.Exec(
			"ALTER TABLE web_pages ADD COLUMN format text NOT NULL DEFAULT 'html'",
		).Error; err != nil {
			return fmt.Errorf("failed to add webpage format column: %w", err)
		}
	}
	if hadWebPageTable && !hadWebPagePublished {
		if err := gormDB.Exec(
			"ALTER TABLE web_pages ADD COLUMN published numeric NOT NULL DEFAULT 1",
		).Error; err != nil {
			return fmt.Errorf("failed to add webpage published column: %w", err)
		}
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

	if !hadWebPageFormat {
		if err := gormDB.Model(&models.WebPage{}).
			Where("format = ''").
			Update("format", models.WebPageFormatHTML).Error; err != nil {
			return fmt.Errorf("failed to migrate webpage formats: %w", err)
		}
	}
	if !hadWebPagePublished {
		if err := gormDB.Model(&models.WebPage{}).
			Where("published = ?", false).
			Update("published", true).Error; err != nil {
			return fmt.Errorf("failed to migrate webpage visibility: %w", err)
		}
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

func hasWebPageColumn(gormDB *gorm.DB, column string) (bool, error) {
	if !gormDB.Migrator().HasTable("web_pages") {
		return false, nil
	}

	var count int64
	if err := gormDB.Raw(
		"SELECT COUNT(*) FROM pragma_table_info('web_pages') WHERE name = ?",
		column,
	).Scan(&count).Error; err != nil {
		return false, fmt.Errorf("failed to inspect webpage schema: %w", err)
	}
	return count > 0, nil
}
