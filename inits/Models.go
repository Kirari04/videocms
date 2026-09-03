package inits

import (
	"ch/kirari04/videocms/background"
	"ch/kirari04/videocms/models"
	"errors"
	"fmt"
	"time"

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
		&models.StorageMount{},
		&models.StoragePool{},
		&models.StoragePoolMount{},
		&models.StorageCacheEntry{},
		&models.StorageMigration{},
		&models.StorageMigrationAccount{},
		&models.StorageMigrationItem{},
		&models.StorageMigrationObject{},
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
		&models.DownloadJob{},
	); err != nil {
		return err
	}
	// The first cache release added this nullable column through AutoMigrate.
	// Existing file rows therefore retained NULL while newly inserted rows used
	// Go's zero value. A NULL generation makes otherwise valid legacy media
	// silently ineligible for read caching, so normalize every row on upgrade.
	if err := gormDB.Unscoped().Model(&models.File{}).
		Where("storage_cache_version IS NULL").
		UpdateColumn("storage_cache_version", 0).Error; err != nil {
		return fmt.Errorf("failed to backfill file storage cache versions: %w", err)
	}
	if err := backfillTrafficAccounting(gormDB); err != nil {
		return err
	}
	if !gormDB.Migrator().HasIndex(&models.TrafficLog{}, "idx_traffic_logs_storage_bucket") {
		if err := gormDB.Exec(`CREATE INDEX idx_traffic_logs_storage_bucket
			ON traffic_logs (delivery_source, bucket_start)`).Error; err != nil {
			return fmt.Errorf("failed to index storage traffic attribution: %w", err)
		}
	}
	if gormDB.Migrator().HasIndex(&models.TrafficLog{}, "idx_traffic_logs_storage_window") {
		if err := gormDB.Migrator().DropIndex(&models.TrafficLog{}, "idx_traffic_logs_storage_window"); err != nil {
			return fmt.Errorf("failed to remove obsolete storage traffic index: %w", err)
		}
	}
	if err := background.Migrate(gormDB); err != nil {
		return fmt.Errorf("failed to migrate background work tables: %w", err)
	}
	if err := background.BackfillLegacy(gormDB, time.Now()); err != nil {
		return fmt.Errorf("failed to backfill background work: %w", err)
	}

	if err := gormDB.Model(&models.TrafficLog{}).
		Where("source IS NULL OR source = ''").
		Update("source", models.TrafficSourcePlayer).Error; err != nil {
		return fmt.Errorf("failed to backfill traffic source: %w", err)
	}
	if err := gormDB.Unscoped().Model(&models.File{}).
		Where("storage_id IS NULL OR storage_id = ''").
		Update("storage_id", "local").Error; err != nil {
		return fmt.Errorf("failed to backfill file storage IDs: %w", err)
	}
	if err := gormDB.Unscoped().Model(&models.File{}).
		Where("storage_state IS NULL OR storage_state = ''").
		Update("storage_state", models.FileStorageAvailable).Error; err != nil {
		return fmt.Errorf("failed to backfill file storage state: %w", err)
	}
	if err := ensureLocalStorageDefaults(gormDB); err != nil {
		return err
	}
	if err := backfillFileStoragePools(gormDB); err != nil {
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

// backfillTrafficAccounting limits each write transaction so upgrading an
// installation with a large traffic history does not monopolize SQLite's
// single writer for the entire migration.
func backfillTrafficAccounting(gormDB *gorm.DB) error {
	const batchSize = 5000
	type trafficRow struct {
		ID uint
	}
	for {
		var rows []trafficRow
		if err := gormDB.Unscoped().Model(&models.TrafficLog{}).
			Select("id").
			Where("request_count IS NULL OR request_count = 0 OR bucket_start IS NULL OR bucket_start = 0").
			Order("id ASC").
			Limit(batchSize).
			Find(&rows).Error; err != nil {
			return fmt.Errorf("failed to inspect legacy traffic accounting: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}

		ids := make([]uint, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		if err := gormDB.Unscoped().Model(&models.TrafficLog{}).
			Where("id IN ?", ids).
			Updates(map[string]any{
				"request_count": gorm.Expr("CASE WHEN request_count IS NULL OR request_count = 0 THEN 1 ELSE request_count END"),
				"bucket_start":  gorm.Expr("CASE WHEN bucket_start IS NULL OR bucket_start = 0 THEN CAST(strftime('%s', COALESCE(created_at, CURRENT_TIMESTAMP)) AS INTEGER) / 60 * 60 ELSE bucket_start END"),
			}).Error; err != nil {
			return fmt.Errorf("failed to backfill legacy traffic accounting: %w", err)
		}
	}
}

func ensureLocalStorageDefaults(gormDB *gorm.DB) error {
	localMount := models.StorageMount{
		UUID:     models.StorageMountLocalUUID,
		Name:     "Local storage",
		Provider: models.StorageProviderLocal,
		Mounted:  true,
		System:   true,
	}
	if err := gormDB.Where("uuid = ?", localMount.UUID).FirstOrCreate(&localMount).Error; err != nil {
		return fmt.Errorf("failed to initialize local storage mount: %w", err)
	}
	localPool := models.StoragePool{
		UUID:   models.StoragePoolLocalUUID,
		Name:   "Local uploads",
		System: true,
	}
	if err := gormDB.Where("uuid = ?", localPool.UUID).FirstOrCreate(&localPool).Error; err != nil {
		return fmt.Errorf("failed to initialize local storage pool: %w", err)
	}
	membership := models.StoragePoolMount{
		StoragePoolID: localPool.ID, StorageMountID: localMount.ID, Role: models.StoragePoolMountPrimary,
	}
	if err := gormDB.Where("storage_pool_id = ? AND storage_mount_id = ?", localPool.ID, localMount.ID).
		FirstOrCreate(&membership).Error; err != nil {
		return fmt.Errorf("failed to initialize local storage pool membership: %w", err)
	}
	if err := gormDB.Model(&models.StoragePoolMount{}).
		Where("role IS NULL OR role = ''").
		Update("role", models.StoragePoolMountPrimary).Error; err != nil {
		return fmt.Errorf("failed to backfill storage pool mount roles: %w", err)
	}
	var defaultPoolCount int64
	if err := gormDB.Model(&models.StoragePool{}).Where("is_default = ?", true).Count(&defaultPoolCount).Error; err != nil {
		return fmt.Errorf("failed to inspect default storage pool: %w", err)
	}
	if defaultPoolCount == 0 {
		if err := gormDB.Model(&localPool).Update("is_default", true).Error; err != nil {
			return fmt.Errorf("failed to select local storage pool: %w", err)
		}
	}
	return nil
}

func backfillFileStoragePools(gormDB *gorm.DB) error {
	type candidate struct {
		StorageID string
		PoolID    uint
		Count     int64
	}
	var candidates []candidate
	if err := gormDB.Table("storage_mounts AS mounts").
		Select("mounts.uuid AS storage_id, MIN(members.storage_pool_id) AS pool_id, COUNT(*) AS count").
		Joins("JOIN storage_pool_mounts AS members ON members.storage_mount_id = mounts.id").
		Where("members.role = ?", models.StoragePoolMountPrimary).
		Group("mounts.uuid").
		Scan(&candidates).Error; err != nil {
		return fmt.Errorf("inspect file storage pool candidates: %w", err)
	}
	for _, candidate := range candidates {
		if candidate.Count != 1 {
			continue
		}
		if err := gormDB.Unscoped().Model(&models.File{}).
			Where("storage_pool_id IS NULL AND storage_id = ?", candidate.StorageID).
			Update("storage_pool_id", candidate.PoolID).Error; err != nil {
			return fmt.Errorf("backfill file storage pool for %s: %w", candidate.StorageID, err)
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
