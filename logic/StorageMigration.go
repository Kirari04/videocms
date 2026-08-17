package logic

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"ch/kirari04/videocms/background"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrStorageMigrationConflict    = errors.New("storage migration conflicts with existing work")
	ErrStorageMigrationEmpty       = errors.New("source pool contains no available videos")
	ErrStorageMigrationUnavailable = errors.New("storage migration requires available mounts and media")
)

type StorageMigrationInput struct {
	SourcePoolID      uint   `json:"sourcePoolId" validate:"required"`
	DestinationPoolID uint   `json:"destinationPoolId" validate:"required"`
	PlanFingerprint   string `json:"planFingerprint,omitempty"`
	IdempotencyKey    string `json:"-"`
}

type StorageMigrationMountPreview struct {
	ID        uint   `json:"id"`
	UUID      string `json:"uuid"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	UsedBytes int64  `json:"usedBytes"`
}

type StorageMigrationPlacementPreview struct {
	MountID      string `json:"mountId"`
	MountName    string `json:"mountName"`
	FileCount    int64  `json:"fileCount"`
	PlannedBytes int64  `json:"plannedBytes"`
}

type StorageMigrationPreview struct {
	SourcePoolID          uint                               `json:"sourcePoolId"`
	SourcePoolName        string                             `json:"sourcePoolName"`
	DestinationPoolID     uint                               `json:"destinationPoolId"`
	DestinationPoolName   string                             `json:"destinationPoolName"`
	FileCount             int64                              `json:"fileCount"`
	PlannedBytes          int64                              `json:"plannedBytes"`
	SourceMounts          []StorageMigrationMountPreview     `json:"sourceMounts"`
	DestinationMounts     []StorageMigrationMountPreview     `json:"destinationMounts"`
	DestinationPlacements []StorageMigrationPlacementPreview `json:"destinationPlacements"`
	Warnings              []string                           `json:"warnings"`
	CleanupGraceHours     int                                `json:"cleanupGraceHours"`
	PlanFingerprint       string                             `json:"planFingerprint"`
}

type StorageMigrationSummary struct {
	Active             int64 `json:"active"`
	RetainingOriginals int64 `json:"retainingOriginals"`
	NeedsAttention     int64 `json:"needsAttention"`
	VideosMoved        int64 `json:"videosMoved"`
}

type storageMigrationPlan struct {
	preview     StorageMigrationPreview
	files       []models.File
	destination map[uint]string
}

type storageMountUsageRow struct {
	UUID      string
	UsedBytes int64
}

func (s *Service) PreviewStorageMigration(ctx context.Context, input StorageMigrationInput) (StorageMigrationPreview, error) {
	plan, err := s.buildStorageMigrationPlan(ctx, input)
	if err != nil {
		return StorageMigrationPreview{}, err
	}
	return plan.preview, nil
}

func (s *Service) StartStorageMigration(ctx context.Context, input StorageMigrationInput, actorID uint, actorName string) (*models.StorageMigration, *background.Job, error) {
	if s == nil || s.Deps == nil || s.Deps.Background == nil {
		return nil, nil, errors.New("background work is not configured")
	}
	requestKey := storageMigrationRequestKey(actorID, input.IdempotencyKey)
	if requestKey == "" {
		return nil, nil, fmt.Errorf("%w: an idempotency key is required", ErrStorageMigrationConflict)
	}
	if migration, job, found, err := s.storageMigrationByRequest(ctx, requestKey, input); found || err != nil {
		return migration, job, err
	}
	releasePools := s.Deps.StorageLifecycle.PoolReadLocks(input.SourcePoolID, input.DestinationPoolID)
	defer releasePools()
	plan, err := s.buildStorageMigrationPlan(ctx, input)
	if err != nil {
		return nil, nil, err
	}
	mountIDs := make([]string, 0, len(plan.preview.SourceMounts)+len(plan.preview.DestinationMounts))
	for _, mount := range plan.preview.SourceMounts {
		mountIDs = append(mountIDs, mount.UUID)
	}
	for _, mount := range plan.preview.DestinationMounts {
		mountIDs = append(mountIDs, mount.UUID)
	}
	releaseMounts := s.Deps.StorageLifecycle.ReadLocks(mountIDs...)
	defer releaseMounts()
	fileIDs := make([]uint, 0, len(plan.files))
	for _, file := range plan.files {
		fileIDs = append(fileIDs, file.ID)
	}
	releaseFiles := s.Deps.StorageLifecycle.FileReadLocks(fileIDs...)
	defer releaseFiles()
	validatedPlan, err := s.buildStorageMigrationPlan(ctx, input)
	if err != nil {
		return nil, nil, err
	}
	if !sameStorageMigrationFiles(plan.files, validatedPlan.files) {
		return nil, nil, fmt.Errorf("%w: source videos changed during migration setup; review the preview again", ErrStorageMigrationConflict)
	}
	plan = validatedPlan
	wantedFingerprint := strings.TrimSpace(input.PlanFingerprint)
	if wantedFingerprint == "" || wantedFingerprint != plan.preview.PlanFingerprint {
		return nil, nil, fmt.Errorf("%w: the migration preview changed; review it again before starting", ErrStorageMigrationConflict)
	}
	now := time.Now().UTC()
	migration := &models.StorageMigration{
		UUID:                uuid.NewString(),
		RequestKey:          requestKey,
		PlanFingerprint:     plan.preview.PlanFingerprint,
		SourcePoolID:        input.SourcePoolID,
		DestinationPoolID:   input.DestinationPoolID,
		SourcePoolName:      plan.preview.SourcePoolName,
		DestinationPoolName: plan.preview.DestinationPoolName,
		Status:              models.StorageMigrationQueued,
		Phase:               "Waiting to start",
		FileCount:           plan.preview.FileCount,
		PlannedBytes:        plan.preview.PlannedBytes,
		CreatedByID:         actorID,
		CreatedByName:       strings.TrimSpace(actorName),
	}
	err = s.Deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(migration).Error; err != nil {
			return err
		}
		items := make([]models.StorageMigrationItem, 0, len(plan.files))
		for _, file := range plan.files {
			items = append(items, models.StorageMigrationItem{
				MigrationID: migration.ID, FileID: file.ID, FileUUID: file.UUID,
				SourceMountID: file.StorageID, DestinationMountID: plan.destination[file.ID],
				Status: models.StorageMigrationItemPending, ReservationKey: fmt.Sprintf("file:%d", file.ID),
				PlannedBytes: file.Size, ProgressMessage: "Waiting to copy",
			})
		}
		if err := tx.CreateInBatches(&items, 100).Error; err != nil {
			if isStorageMigrationUniqueError(err) {
				return ErrStorageMigrationConflict
			}
			return err
		}
		return nil
	})
	if err != nil {
		if isStorageMigrationUniqueError(err) {
			if existing, job, found, loadErr := s.storageMigrationByRequest(context.WithoutCancel(ctx), requestKey, input); found || loadErr != nil {
				return existing, job, loadErr
			}
		}
		return nil, nil, err
	}

	job, enqueueErr := s.ensureStorageMigrationJob(ctx, migration)
	if enqueueErr != nil {
		_ = s.Deps.DB.WithContext(context.WithoutCancel(ctx)).Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&models.StorageMigrationItem{}).Where("migration_id = ?", migration.ID).Update("reservation_key", "").Error; err != nil {
				return err
			}
			return tx.Model(migration).Updates(map[string]any{
				"status": models.StorageMigrationFailed, "phase": "Could not queue migration",
				"error_code": "queue_failed", "error_message": enqueueErr.Error(),
			}).Error
		})
		return nil, nil, enqueueErr
	}
	if err := s.Deps.DB.WithContext(context.WithoutCancel(ctx)).Model(migration).Updates(map[string]any{
		"background_job_id": job.ID, "phase": "Waiting to start", "started_at": &now,
	}).Error; err != nil {
		// The queued task also repairs this association when it starts. Once the
		// durable job exists, returning an error would encourage a duplicate start.
		migration.Phase = "Waiting to start"
	}
	migration.BackgroundJobID = job.ID
	migration.StartedAt = &now
	return migration, job, nil
}

func storageMigrationRequestKey(actorID uint, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", actorID, value)))
	return fmt.Sprintf("storage-migration-request:%x", sum)
}

func (s *Service) storageMigrationByRequest(ctx context.Context, requestKey string, input StorageMigrationInput) (*models.StorageMigration, *background.Job, bool, error) {
	var migration models.StorageMigration
	err := s.Deps.DB.WithContext(ctx).Where("request_key = ?", requestKey).First(&migration).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	if migration.SourcePoolID != input.SourcePoolID || migration.DestinationPoolID != input.DestinationPoolID ||
		migration.PlanFingerprint != strings.TrimSpace(input.PlanFingerprint) {
		return nil, nil, true, fmt.Errorf("%w: the idempotency key was already used for another migration request", ErrStorageMigrationConflict)
	}
	job, err := s.ensureStorageMigrationJob(context.WithoutCancel(ctx), &migration)
	if err == nil && migration.Status == models.StorageMigrationFailed && migration.ErrorCode == "queue_failed" {
		now := time.Now().UTC()
		err = s.Deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&migration).Updates(map[string]any{
			"status": models.StorageMigrationQueued, "phase": "Waiting to start", "started_at": &now,
			"background_job_id": job.ID, "error_code": "", "error_message": "",
		}).Error
		if err == nil {
			migration.Status = models.StorageMigrationQueued
			migration.Phase = "Waiting to start"
			migration.StartedAt = &now
			migration.BackgroundJobID = job.ID
			migration.ErrorCode = ""
			migration.ErrorMessage = ""
		}
	}
	return &migration, job, true, err
}

func storageMigrationJobSpec(migration models.StorageMigration) background.JobSpec {
	return background.JobSpec{
		Kind: "storage.migration", Visibility: background.VisibilityAdmin,
		SubjectType: "storage_migration", SubjectID: migration.UUID,
		IdempotencyKey: "storage-migration:" + migration.UUID,
		Label:          fmt.Sprintf("Migrate %s to %s", migration.SourcePoolName, migration.DestinationPoolName),
		Pausable:       true,
		Tasks: []background.TaskSpec{{
			Kind: "storage.migration.run", Queue: background.QueueStorage, Phase: "Migrating videos",
			Payload: map[string]any{"migrationId": migration.ID}, DedupeKey: fmt.Sprintf("storage-migration:%d", migration.ID),
			Priority: 20, Required: true, Weight: 1, MaxAttempts: 4,
		}},
	}
}

func (s *Service) ensureStorageMigrationJob(ctx context.Context, migration *models.StorageMigration) (*background.Job, error) {
	if migration == nil || migration.ID == 0 || s.Deps.Background == nil {
		return nil, errors.New("background work is not configured")
	}
	if migration.BackgroundJobID != "" {
		if detail, err := s.Deps.Background.Job(ctx, migration.BackgroundJobID, nil, true); err == nil {
			return &detail.Job, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	var existing background.Job
	if err := s.Deps.DB.WithContext(ctx).
		Where("kind = ? AND subject_type = ? AND subject_id = ?", "storage.migration", "storage_migration", migration.UUID).
		Order("created_at DESC").First(&existing).Error; err == nil {
		// The worker repairs this association when it starts. Once the durable
		// job exists, an association write failure must not release reservations
		// or encourage the caller to create another migration.
		_ = s.Deps.DB.WithContext(context.WithoutCancel(ctx)).Model(migration).Update("background_job_id", existing.ID).Error
		migration.BackgroundJobID = existing.ID
		detail, err := s.Deps.Background.Job(context.WithoutCancel(ctx), existing.ID, nil, true)
		if err != nil {
			return nil, err
		}
		return &detail.Job, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	job, _, err := s.Deps.Background.Enqueue(ctx, storageMigrationJobSpec(*migration))
	if err != nil {
		return nil, err
	}
	_ = s.Deps.DB.WithContext(context.WithoutCancel(ctx)).Model(migration).Update("background_job_id", job.ID).Error
	migration.BackgroundJobID = job.ID
	return job, nil
}

func (s *Service) EnsureStorageMigrationJob(ctx context.Context, migrationID uint) (*background.Job, error) {
	var migration models.StorageMigration
	if err := s.Deps.DB.WithContext(ctx).First(&migration, migrationID).Error; err != nil {
		return nil, err
	}
	return s.ensureStorageMigrationJob(ctx, &migration)
}

func sameStorageMigrationFiles(first, second []models.File) bool {
	if len(first) != len(second) {
		return false
	}
	ids := make(map[uint]struct{}, len(first))
	for _, file := range first {
		ids[file.ID] = struct{}{}
	}
	for _, file := range second {
		if _, ok := ids[file.ID]; !ok {
			return false
		}
	}
	return true
}

func (s *Service) buildStorageMigrationPlan(ctx context.Context, input StorageMigrationInput) (storageMigrationPlan, error) {
	if s == nil || s.Deps == nil || s.Deps.DB == nil || s.Deps.Storage == nil {
		return storageMigrationPlan{}, storage.ErrStoreNotConfigured
	}
	if input.SourcePoolID == 0 || input.DestinationPoolID == 0 || input.SourcePoolID == input.DestinationPoolID {
		return storageMigrationPlan{}, fmt.Errorf("%w: source and destination pools must be different", ErrStorageMigrationConflict)
	}
	var sourcePool, destinationPool models.StoragePool
	preload := func(db *gorm.DB) *gorm.DB { return db.Preload("Members.StorageMount") }
	if err := preload(s.Deps.DB.WithContext(ctx)).First(&sourcePool, input.SourcePoolID).Error; err != nil {
		return storageMigrationPlan{}, err
	}
	if err := preload(s.Deps.DB.WithContext(ctx)).First(&destinationPool, input.DestinationPoolID).Error; err != nil {
		return storageMigrationPlan{}, err
	}
	sourceMounts, sourceIDs, err := s.storageMigrationMounts(ctx, sourcePool)
	if err != nil {
		return storageMigrationPlan{}, err
	}
	destinationMounts, destinationIDs, err := s.storageMigrationMounts(ctx, destinationPool)
	if err != nil {
		return storageMigrationPlan{}, err
	}
	seen := make(map[string]bool, len(sourceIDs))
	for _, id := range sourceIDs {
		seen[id] = true
	}
	for _, id := range destinationIDs {
		if seen[id] {
			return storageMigrationPlan{}, fmt.Errorf("%w: source and destination pools share mount %s", ErrStorageMigrationConflict, id)
		}
	}

	var unavailable int64
	if err := s.Deps.DB.WithContext(ctx).Model(&models.File{}).
		Where("storage_id IN ? AND storage_state = ?", sourceIDs, models.FileStorageUnavailable).
		Count(&unavailable).Error; err != nil {
		return storageMigrationPlan{}, err
	}
	if unavailable > 0 {
		return storageMigrationPlan{}, fmt.Errorf("%w: source pool contains %d unavailable videos", ErrStorageMigrationUnavailable, unavailable)
	}
	var files []models.File
	if err := s.Deps.DB.WithContext(ctx).
		Where("storage_id IN ? AND storage_state = ?", sourceIDs, models.FileStorageAvailable).
		Order("size DESC, id ASC").Find(&files).Error; err != nil {
		return storageMigrationPlan{}, err
	}
	if len(files) == 0 {
		return storageMigrationPlan{}, ErrStorageMigrationEmpty
	}
	var plannedBytes int64
	for _, file := range files {
		plannedBytes += file.Size
	}
	var reserved int64
	if err := s.Deps.DB.WithContext(ctx).Model(&models.StorageMigrationItem{}).
		Where(`reservation_key <> '' AND EXISTS (
			SELECT 1 FROM files
			WHERE files.id = storage_migration_items.file_id
			AND files.deleted_at IS NULL
			AND files.storage_id IN ?
			AND files.storage_state = ?
		)`, sourceIDs, models.FileStorageAvailable).Count(&reserved).Error; err != nil {
		return storageMigrationPlan{}, err
	}
	if reserved > 0 {
		return storageMigrationPlan{}, fmt.Errorf("%w: %d videos are already reserved by another migration", ErrStorageMigrationConflict, reserved)
	}

	usage := make(map[string]int64, len(destinationIDs))
	var usageRows []storageMountUsageRow
	if err := s.Deps.DB.WithContext(ctx).Model(&models.File{}).
		Select("storage_id AS uuid, COALESCE(SUM(size), 0) AS used_bytes").
		Where("storage_id IN ? AND storage_state = ?", destinationIDs, models.FileStorageAvailable).
		Group("storage_id").Scan(&usageRows).Error; err != nil {
		return storageMigrationPlan{}, err
	}
	for _, id := range destinationIDs {
		usage[id] = 0
	}
	for _, row := range usageRows {
		usage[row.UUID] = row.UsedBytes
	}
	destination := make(map[uint]string, len(files))
	placementCounts := make(map[string]int64, len(destinationIDs))
	placementBytes := make(map[string]int64, len(destinationIDs))
	for _, file := range files {
		selected := destinationIDs[0]
		for _, candidate := range destinationIDs[1:] {
			if usage[candidate] < usage[selected] || (usage[candidate] == usage[selected] && candidate < selected) {
				selected = candidate
			}
		}
		destination[file.ID] = selected
		usage[selected] += file.Size
		placementCounts[selected]++
		placementBytes[selected] += file.Size
	}
	mountNames := make(map[string]string, len(destinationMounts))
	for _, mount := range destinationMounts {
		mountNames[mount.UUID] = mount.Name
	}
	placements := make([]StorageMigrationPlacementPreview, 0, len(destinationIDs))
	for _, id := range destinationIDs {
		placements = append(placements, StorageMigrationPlacementPreview{
			MountID: id, MountName: mountNames[id], FileCount: placementCounts[id], PlannedBytes: placementBytes[id],
		})
	}

	warnings := []string{"Videos uploaded after this snapshot will not be included.", "Upload routing is not changed automatically."}
	if sourcePool.IsDefault {
		warnings = append(warnings, "The source is still the default upload pool.")
	}
	var overrideCount int64
	if err := s.Deps.DB.WithContext(ctx).Model(&models.User{}).Where("storage_pool_id = ?", sourcePool.ID).Count(&overrideCount).Error; err != nil {
		return storageMigrationPlan{}, err
	}
	if overrideCount > 0 {
		warnings = append(warnings, fmt.Sprintf("%d users still upload to the source pool.", overrideCount))
	}
	plan := storageMigrationPlan{
		preview: StorageMigrationPreview{
			SourcePoolID: sourcePool.ID, SourcePoolName: sourcePool.Name,
			DestinationPoolID: destinationPool.ID, DestinationPoolName: destinationPool.Name,
			FileCount: int64(len(files)), PlannedBytes: plannedBytes,
			SourceMounts: sourceMounts, DestinationMounts: destinationMounts,
			DestinationPlacements: placements, Warnings: warnings, CleanupGraceHours: 24,
		},
		files: files, destination: destination,
	}
	plan.preview.PlanFingerprint = storageMigrationPlanFingerprint(plan)
	return plan, nil
}

func storageMigrationPlanFingerprint(plan storageMigrationPlan) string {
	digest := sha256.New()
	_, _ = fmt.Fprintf(digest, "source-pool:%d\ndestination-pool:%d\n", plan.preview.SourcePoolID, plan.preview.DestinationPoolID)
	for _, mount := range plan.preview.SourceMounts {
		_, _ = fmt.Fprintf(digest, "source-mount:%s\n", mount.UUID)
	}
	for _, mount := range plan.preview.DestinationMounts {
		_, _ = fmt.Fprintf(digest, "destination-mount:%s\n", mount.UUID)
	}
	files := append([]models.File(nil), plan.files...)
	sort.Slice(files, func(i, j int) bool { return files[i].ID < files[j].ID })
	for _, file := range files {
		_, _ = fmt.Fprintf(digest, "file:%d:%s:%s:%d:%s\n", file.ID, file.UUID, file.StorageID, file.Size, plan.destination[file.ID])
	}
	return fmt.Sprintf("%x", digest.Sum(nil))
}

func (s *Service) storageMigrationMounts(ctx context.Context, pool models.StoragePool) ([]StorageMigrationMountPreview, []string, error) {
	if len(pool.Members) == 0 {
		return nil, nil, fmt.Errorf("%w: pool %s has no mounts", ErrStorageMigrationUnavailable, pool.Name)
	}
	mounts := make([]StorageMigrationMountPreview, 0, len(pool.Members))
	ids := make([]string, 0, len(pool.Members))
	for _, member := range pool.Members {
		mount := member.StorageMount
		if !mount.Mounted || strings.TrimSpace(mount.LastError) != "" {
			return nil, nil, fmt.Errorf("%w: mount %s is not available", ErrStorageMigrationUnavailable, mount.Name)
		}
		if _, err := s.Deps.Storage.Store(mount.UUID); err != nil {
			return nil, nil, fmt.Errorf("%w: mount %s is not connected", ErrStorageMigrationUnavailable, mount.Name)
		}
		var usedBytes int64
		if err := s.Deps.DB.WithContext(ctx).Model(&models.File{}).Where("storage_id = ? AND storage_state = ?", mount.UUID, models.FileStorageAvailable).
			Select("COALESCE(SUM(size), 0)").Scan(&usedBytes).Error; err != nil {
			return nil, nil, err
		}
		mounts = append(mounts, StorageMigrationMountPreview{
			ID: mount.ID, UUID: mount.UUID, Name: mount.Name, Provider: mount.Provider, UsedBytes: usedBytes,
		})
		ids = append(ids, mount.UUID)
	}
	sort.Slice(mounts, func(i, j int) bool { return mounts[i].UUID < mounts[j].UUID })
	sort.Strings(ids)
	return mounts, ids, nil
}

func isStorageMigrationUniqueError(err error) bool {
	message := strings.ToLower(err.Error())
	return errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate key")
}

func StorageMigrationStatusesForFilter(filter string) ([]string, bool) {
	switch strings.TrimSpace(filter) {
	case "":
		return nil, true
	case "active":
		return []string{models.StorageMigrationQueued, models.StorageMigrationRunning, models.StorageMigrationPaused}, true
	case "retention":
		return []string{models.StorageMigrationRetainingOriginals, models.StorageMigrationCleaningOriginals, models.StorageMigrationOriginalsRetained}, true
	case "attention":
		return []string{models.StorageMigrationFailed, models.StorageMigrationCanceled}, true
	case "complete":
		return []string{models.StorageMigrationCompleted}, true
	default:
		return nil, false
	}
}

func (s *Service) ListStorageMigrations(ctx context.Context, filter string, limit int, beforeID uint) ([]models.StorageMigration, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	statuses, valid := StorageMigrationStatusesForFilter(filter)
	if !valid {
		return nil, fmt.Errorf("%w: unknown migration status filter", ErrStorageMigrationConflict)
	}
	query := s.Deps.DB.WithContext(ctx).Model(&models.StorageMigration{})
	if len(statuses) > 0 {
		query = query.Where("status IN ?", statuses)
	}
	if beforeID > 0 {
		query = query.Where("id < ?", beforeID)
	}
	var migrations []models.StorageMigration
	err := query.Order("id DESC").Limit(limit).Find(&migrations).Error
	return migrations, err
}

func (s *Service) GetStorageMigrationSummary(ctx context.Context) (StorageMigrationSummary, error) {
	var summary StorageMigrationSummary
	err := s.Deps.DB.WithContext(ctx).Model(&models.StorageMigration{}).
		Select(`COALESCE(SUM(CASE WHEN status IN ? THEN 1 ELSE 0 END), 0) AS active,
			COALESCE(SUM(CASE WHEN status IN ? THEN 1 ELSE 0 END), 0) AS retaining_originals,
			COALESCE(SUM(CASE WHEN status IN ? THEN 1 ELSE 0 END), 0) AS needs_attention,
			COALESCE(SUM(cutover_count), 0) AS videos_moved`,
			[]string{models.StorageMigrationQueued, models.StorageMigrationRunning, models.StorageMigrationPaused},
			[]string{models.StorageMigrationRetainingOriginals, models.StorageMigrationCleaningOriginals},
			[]string{models.StorageMigrationFailed, models.StorageMigrationCanceled}).
		Scan(&summary).Error
	return summary, err
}

func (s *Service) GetStorageMigration(ctx context.Context, migrationUUID string) (models.StorageMigration, error) {
	var migration models.StorageMigration
	err := s.Deps.DB.WithContext(ctx).Where("uuid = ?", strings.TrimSpace(migrationUUID)).First(&migration).Error
	if err != nil {
		return models.StorageMigration{}, err
	}
	err = s.Deps.DB.WithContext(context.WithoutCancel(ctx)).First(&migration, migration.ID).Error
	return migration, err
}

type StorageMigrationItemView struct {
	models.StorageMigrationItem
	VideoName string `json:"VideoName"`
}

func (s *Service) ListStorageMigrationItems(ctx context.Context, migrationID uint, status string, limit int, afterID uint) ([]StorageMigrationItemView, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	query := s.Deps.DB.WithContext(ctx).Table("storage_migration_items AS migration_items").
		Select("migration_items.*, COALESCE(MIN(links.name), migration_items.file_uuid) AS video_name").
		Joins("LEFT JOIN links ON links.file_id = migration_items.file_id AND links.deleted_at IS NULL").
		Where("migration_items.migration_id = ? AND migration_items.deleted_at IS NULL", migrationID)
	if status = strings.TrimSpace(status); status != "" {
		query = query.Where("migration_items.status = ?", status)
	}
	if afterID > 0 {
		query = query.Where("migration_items.id > ?", afterID)
	}
	var items []StorageMigrationItemView
	err := query.Group("migration_items.id").Order("migration_items.id ASC").Limit(limit).Scan(&items).Error
	return items, err
}

func (s *Service) ensureStorageMountNotMigrating(mountUUID string) error {
	if !s.Deps.DB.Migrator().HasTable(&models.StorageMigrationItem{}) {
		return nil
	}
	var count int64
	err := s.Deps.DB.Model(&models.StorageMigrationItem{}).
		Joins("JOIN storage_migrations ON storage_migrations.id = storage_migration_items.migration_id AND storage_migrations.deleted_at IS NULL").
		Where("(source_mount_id = ? OR destination_mount_id = ?) AND (reservation_key <> '' OR (storage_migrations.status IN ? AND storage_migrations.cleanup_after > ?))", mountUUID, mountUUID, []string{models.StorageMigrationCanceled, models.StorageMigrationOriginalsRetained}, time.Now().UTC()).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: storage mount is reserved by an active migration", ErrStorageMigrationConflict)
	}
	return nil
}

func (s *Service) ensureStoragePoolNotMigrating(poolID uint) error {
	if !s.Deps.DB.Migrator().HasTable(&models.StorageMigration{}) {
		return nil
	}
	var count int64
	err := s.Deps.DB.Model(&models.StorageMigration{}).
		Where("(source_pool_id = ? OR destination_pool_id = ?) AND status NOT IN ?", poolID, poolID, []string{
			models.StorageMigrationCompleted, models.StorageMigrationCanceled, models.StorageMigrationOriginalsRetained,
		}).Count(&count).Error
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: storage pool is used by an active migration", ErrStorageMigrationConflict)
	}
	return nil
}

func (s *Service) KeepStorageMigrationOriginals(ctx context.Context, migrationUUID string, actorID uint, actorName string) (models.StorageMigration, error) {
	migration, err := s.GetStorageMigration(ctx, migrationUUID)
	if err != nil {
		return models.StorageMigration{}, err
	}
	if migration.Status != models.StorageMigrationRetainingOriginals && migration.Status != models.StorageMigrationCleaningOriginals && migration.Status != models.StorageMigrationPaused {
		return models.StorageMigration{}, fmt.Errorf("%w: originals can only be retained after video cutover", ErrStorageMigrationConflict)
	}
	if migration.Status == models.StorageMigrationPaused && migration.CleanupJobID == "" {
		return models.StorageMigration{}, fmt.Errorf("%w: the video migration is paused before cleanup", ErrStorageMigrationConflict)
	}
	if migration.CleanupJobID != "" && s.Deps.Background != nil {
		detail, jobErr := s.Deps.Background.Job(ctx, migration.CleanupJobID, nil, true)
		if jobErr == nil {
			if detail.Status == background.JobSucceeded || detail.Status == background.JobSucceededWithWarnings {
				return models.StorageMigration{}, fmt.Errorf("%w: original cleanup has already completed", ErrStorageMigrationConflict)
			}
			if !backgroundJobTerminal(detail.Status) {
				if err := s.Deps.Background.CancelJob(ctx, detail.ID, actorID, actorName); err != nil && !errors.Is(err, background.ErrConflict) {
					return models.StorageMigration{}, err
				}
			}
		}
	}
	durableCtx := context.WithoutCancel(ctx)
	if err := s.Deps.DB.WithContext(durableCtx).Model(&migration).Updates(map[string]any{
		"keep_originals": true, "phase": "Stopping original cleanup",
	}).Error; err != nil {
		return models.StorageMigration{}, err
	}
	var items []models.StorageMigrationItem
	if err := s.Deps.DB.WithContext(durableCtx).Select("id", "file_id", "status").
		Where("migration_id = ?", migration.ID).Order("file_id ASC").Find(&items).Error; err != nil {
		return models.StorageMigration{}, err
	}
	for index := range items {
		item := &items[index]
		releaseFile := s.Deps.StorageLifecycle.FileWriteLock(item.FileID)
		err := s.Deps.DB.WithContext(durableCtx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Select("id", "status").First(item, item.ID).Error; err != nil {
				return err
			}
			if item.Status == models.StorageMigrationItemCleaned || item.Status == models.StorageMigrationItemDeleted {
				return nil
			}
			status := models.StorageMigrationItemOriginalKept
			message := "Original retained by administrator"
			if item.Status == models.StorageMigrationItemCleaning {
				status = models.StorageMigrationItemOriginalPartial
				message = "Original cleanup was incomplete; remaining data retained"
			}
			return tx.Model(item).
				Where("status NOT IN ?", []string{models.StorageMigrationItemCleaned, models.StorageMigrationItemDeleted}).
				Updates(map[string]any{
					"status": status, "reservation_key": "", "progress_message": message,
				}).Error
		})
		releaseFile()
		if err != nil {
			return models.StorageMigration{}, err
		}
	}
	var cleaned, partial, deleted int64
	now := time.Now().UTC()
	err = s.Deps.DB.WithContext(durableCtx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.StorageMigrationItem{}).
			Where("migration_id = ? AND status = ?", migration.ID, models.StorageMigrationItemCleaned).
			Count(&cleaned).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.StorageMigrationItem{}).
			Where("migration_id = ? AND status = ?", migration.ID, models.StorageMigrationItemOriginalPartial).
			Count(&partial).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.StorageMigrationItem{}).
			Where("migration_id = ? AND status = ?", migration.ID, models.StorageMigrationItemDeleted).
			Count(&deleted).Error; err != nil {
			return err
		}
		phase := "Original copies retained"
		if cleaned > 0 {
			phase = fmt.Sprintf("Originals retained; %d had already been removed", cleaned)
		}
		if partial > 0 {
			phase = fmt.Sprintf("Originals retained; %d may be incomplete after a storage error", partial)
			if cleaned > 0 {
				phase = fmt.Sprintf("Originals retained; %d removed and %d may be incomplete", cleaned, partial)
			}
		}
		return tx.Model(&migration).Updates(map[string]any{
			"status": models.StorageMigrationOriginalsRetained, "phase": phase, "keep_originals": true,
			"cleaned_count": cleaned, "deleted_count": deleted, "completed_at": &now, "error_code": "", "error_message": "",
		}).Error
	})
	if err != nil {
		return models.StorageMigration{}, err
	}
	err = s.Deps.DB.WithContext(context.WithoutCancel(ctx)).First(&migration, migration.ID).Error
	return migration, err
}

func (s *Service) CancelFailedStorageMigration(ctx context.Context, migrationUUID string) (models.StorageMigration, error) {
	migration, err := s.GetStorageMigration(ctx, migrationUUID)
	if err != nil {
		return models.StorageMigration{}, err
	}
	if migration.Status != models.StorageMigrationFailed {
		return models.StorageMigration{}, fmt.Errorf("%w: only a failed migration can be abandoned from this state", ErrStorageMigrationConflict)
	}
	if migration.BackgroundJobID != "" && s.Deps.Background != nil {
		job, jobErr := s.Deps.Background.Job(ctx, migration.BackgroundJobID, nil, true)
		if jobErr != nil {
			return models.StorageMigration{}, jobErr
		}
		if job.Status != background.JobFailed {
			return models.StorageMigration{}, fmt.Errorf("%w: the migration is still retrying", ErrStorageMigrationConflict)
		}
	}
	if err := s.EnsureStorageMigrationAbortCleanup(ctx, migration.ID); err != nil {
		return models.StorageMigration{}, err
	}
	if err := s.Deps.DB.WithContext(context.WithoutCancel(ctx)).First(&migration, migration.ID).Error; err != nil {
		return models.StorageMigration{}, err
	}
	return migration, nil
}

func backgroundJobTerminal(status string) bool {
	switch status {
	case background.JobSucceeded, background.JobSucceededWithWarnings, background.JobFailed, background.JobCanceled:
		return true
	default:
		return false
	}
}

func (s *Service) ReflectStorageMigrationJobControl(ctx context.Context, job background.Job, action string, actorID uint, actorName string) error {
	if job.SubjectType != "storage_migration" || job.SubjectID == "" {
		return nil
	}
	migration, err := s.GetStorageMigration(ctx, job.SubjectID)
	if err != nil {
		return err
	}
	switch action {
	case "pause":
		if job.Kind == "storage.migration.abort_cleanup" {
			return s.Deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&migration).Updates(map[string]any{
				"status": models.StorageMigrationCanceled, "phase": "Canceled; incomplete destination cleanup paused",
			}).Error
		}
		phase := "Migration paused"
		if job.Kind == "storage.migration.cleanup" {
			phase = "Original cleanup paused"
		}
		return s.Deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&migration).Updates(map[string]any{
			"status": models.StorageMigrationPaused, "phase": phase,
		}).Error
	case "resume":
		if job.Kind == "storage.migration.abort_cleanup" {
			return s.Deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&migration).Updates(map[string]any{
				"status": models.StorageMigrationCanceled, "phase": "Canceled; cleaning incomplete destination data",
			}).Error
		}
		status, phase := models.StorageMigrationRunning, "Migrating videos"
		if job.Kind == "storage.migration.cleanup" {
			status, phase = models.StorageMigrationCleaningOriginals, "Cleaning original copies"
			if migration.CleanupAfter != nil && migration.CleanupAfter.After(time.Now().UTC()) {
				status, phase = models.StorageMigrationRetainingOriginals, "Retaining originals until cleanup begins"
			}
		}
		return s.Deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&migration).Updates(map[string]any{
			"status": status, "phase": phase,
		}).Error
	case "cancel":
		if job.Kind == "storage.migration.cleanup" {
			_, err := s.KeepStorageMigrationOriginals(ctx, migration.UUID, actorID, actorName)
			return err
		}
		if job.Kind == "storage.migration" {
			return s.EnsureStorageMigrationAbortCleanup(ctx, migration.ID)
		}
		if job.Kind == "storage.migration.abort_cleanup" {
			return s.Deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&migration).Update("phase", "Canceled; incomplete destination cleanup stopped").Error
		}
	case "retry":
		if job.Kind == "storage.migration" {
			return s.Deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&migration).Updates(map[string]any{
				"status": models.StorageMigrationQueued, "phase": "Waiting to resume migration",
				"cleanup_job_id": "", "cleanup_after": nil, "canceled_at": nil, "keep_originals": false,
				"error_code": "", "error_message": "",
			}).Error
		}
		if job.Kind == "storage.migration.abort_cleanup" {
			return s.Deps.DB.WithContext(context.WithoutCancel(ctx)).Model(&migration).Updates(map[string]any{
				"status": models.StorageMigrationCanceled, "phase": "Canceled; retrying incomplete destination cleanup",
			}).Error
		}
	}
	return nil
}

func (s *Service) EnsureStorageMigrationAbortCleanup(ctx context.Context, migrationID uint) error {
	if s == nil || s.Deps == nil || s.Deps.Background == nil {
		return errors.New("background work is not configured")
	}
	var migration models.StorageMigration
	if err := s.Deps.DB.WithContext(context.WithoutCancel(ctx)).First(&migration, migrationID).Error; err != nil {
		return err
	}
	if migration.CleanupJobID != "" {
		if detail, err := s.Deps.Background.Job(context.WithoutCancel(ctx), migration.CleanupJobID, nil, true); err == nil {
			if detail.Kind == "storage.migration.abort_cleanup" {
				return nil
			}
			if detail.Kind == "storage.migration.cleanup" && !backgroundJobTerminal(detail.Status) {
				if err := s.Deps.Background.CancelJob(context.WithoutCancel(ctx), detail.ID, 0, "VideoCMS"); err != nil && !errors.Is(err, background.ErrConflict) {
					return err
				}
			}
		}
	}
	return s.queueStorageMigrationAbort(ctx, &migration)
}

func (s *Service) queueStorageMigrationAbort(ctx context.Context, migration *models.StorageMigration) error {
	if s.Deps.Background == nil {
		return errors.New("background work is not configured")
	}
	now := time.Now().UTC()
	protectUntil := now.Add(storageMigrationCancellationGrace)
	cancelGeneration := migration.CancelGeneration + 1
	job, _, err := s.Deps.Background.Enqueue(context.WithoutCancel(ctx), background.JobSpec{
		Kind: "storage.migration.abort_cleanup", Visibility: background.VisibilityAdmin,
		SubjectType: "storage_migration", SubjectID: migration.UUID,
		IdempotencyKey: fmt.Sprintf("storage-migration-abort:%s:%d", migration.UUID, cancelGeneration),
		Label:          fmt.Sprintf("Clean canceled migration to %s", migration.DestinationPoolName),
		Pausable:       true,
		Tasks: []background.TaskSpec{{
			Kind: "storage.migration.abort_cleanup", Queue: background.QueueStorage, Phase: "Cleaning canceled migration",
			Payload: map[string]any{"migrationId": migration.ID}, DedupeKey: fmt.Sprintf("storage-abort:%d", migration.ID),
			Priority: 15, Required: true, Weight: 1, MaxAttempts: 8,
		}},
	})
	if err != nil {
		return err
	}
	return s.Deps.DB.WithContext(context.WithoutCancel(ctx)).Model(migration).Updates(map[string]any{
		"status": models.StorageMigrationCanceled, "phase": "Canceled; cleaning incomplete destination data",
		"cleanup_job_id": job.ID, "cleanup_after": &protectUntil, "canceled_at": &now, "keep_originals": true,
		"cancel_generation": cancelGeneration,
	}).Error
}

const storageMigrationCancellationGrace = 24 * time.Hour
