package logic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/storage"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestStorageMountReconnectAndUnmountLifecycle(t *testing.T) {
	db := newStorageAdminTestDB(t)
	localStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remoteLocal, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remoteStore := &adminCloseTrackingStore{Store: remoteLocal}
	storageService, err := storage.NewService("local", storage.LegacyMediaLayout{}, map[string]storage.Store{
		"local":  localStore,
		"remote": remoteStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageService.Close() })
	service := &Service{Deps: &app.Deps{DB: db, Storage: storageService}}

	mount := models.StorageMount{UUID: "remote", Name: "Remote", Provider: models.StorageProviderS3, Mounted: true}
	if err := db.Create(&mount).Error; err != nil {
		t.Fatal(err)
	}
	pool := models.StoragePool{UUID: "pool", Name: "Pool", IsDefault: true}
	if err := db.Create(&pool).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.StoragePoolMount{StoragePoolID: pool.ID, StorageMountID: mount.ID}).Error; err != nil {
		t.Fatal(err)
	}
	found := models.File{UUID: "found-file", StorageID: "missing-old-mount", StorageState: models.FileStorageUnavailable, Size: 10}
	missing := models.File{UUID: "missing-file", StorageID: "missing-old-mount", StorageState: models.FileStorageUnavailable, Size: 20}
	if err := db.Create(&found).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&missing).Error; err != nil {
		t.Fatal(err)
	}
	objectKey, err := storageService.Layout().Video(found.UUID, "720p", "out0.ts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remoteStore.Put(context.Background(), objectKey, strings.NewReader("segment"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}

	preview, err := service.ReconnectStorageMount(context.Background(), mount.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Scanned != 2 || preview.Matched != 1 || preview.Relinked != 0 {
		t.Fatalf("preview = %#v", preview)
	}
	if err := db.First(&found, found.ID).Error; err != nil {
		t.Fatal(err)
	}
	if found.StorageState != models.FileStorageUnavailable || found.StorageID != "missing-old-mount" {
		t.Fatalf("preview changed file = %#v", found)
	}

	applied, err := service.ReconnectStorageMount(context.Background(), mount.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Relinked != 1 {
		t.Fatalf("applied = %#v", applied)
	}
	if err := db.First(&found, found.ID).Error; err != nil {
		t.Fatal(err)
	}
	if found.StorageState != models.FileStorageAvailable || found.StorageID != mount.UUID {
		t.Fatalf("relinked file = %#v", found)
	}

	unavailable, err := service.UnmountStorageMount(mount.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unavailable != 1 {
		t.Fatalf("unavailable files = %d, want 1", unavailable)
	}
	if err := db.First(&found, found.ID).Error; err != nil {
		t.Fatal(err)
	}
	if found.StorageState != models.FileStorageUnavailable {
		t.Fatalf("unmounted file state = %q", found.StorageState)
	}
	if _, err := storageService.Store(mount.UUID); !errors.Is(err, storage.ErrStoreNotConfigured) {
		t.Fatalf("unmounted store error = %v", err)
	}
	if got := remoteStore.CloseCount(); got != 1 {
		t.Fatalf("unmounted store close count = %d, want 1", got)
	}
	var membershipCount int64
	if err := db.Model(&models.StoragePoolMount{}).Where("storage_mount_id = ?", mount.ID).Count(&membershipCount).Error; err != nil {
		t.Fatal(err)
	}
	if membershipCount != 1 {
		t.Fatalf("pool memberships = %d, want retained membership", membershipCount)
	}
}

func TestStorageMountConnectionCanBeTestedWithoutSaving(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		response.Header().Set("Content-Type", "application/xml")
		_, _ = response.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>media</Name><KeyCount>0</KeyCount><MaxKeys>1</MaxKeys><IsTruncated>false</IsTruncated>
</ListBucketResult>`))
	}))
	defer server.Close()

	cipher, err := storage.NewCredentialCipher(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := json.Marshal(storage.S3MountConfiguration{
		Bucket: "media", Region: "us-east-1", Endpoint: server.URL, UsePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	credentials := json.RawMessage(`{"access_key_id":"access","secret_access_key":"secret"}`)
	db := newStorageAdminTestDB(t)
	service := NewService(&app.Deps{DB: db, StorageCipher: cipher})

	if err := service.TestStorageMount(context.Background(), 0, StorageMountInput{
		Provider: models.StorageProviderS3, Configuration: configuration, Credentials: &credentials,
	}); err != nil {
		t.Fatalf("TestStorageMount() error = %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("TestStorageMount() requests = %d, want 1", requests.Load())
	}
	var mountCount int64
	if err := db.Model(&models.StorageMount{}).Count(&mountCount).Error; err != nil {
		t.Fatal(err)
	}
	if mountCount != 0 {
		t.Fatalf("TestStorageMount() saved %d mounts, want 0", mountCount)
	}
}

func TestDeleteStorageMountRemovesConfigurationAndKeepsFilesRecoverable(t *testing.T) {
	db := newStorageAdminTestDB(t)
	mount := models.StorageMount{
		UUID:                 "deleted-mount",
		Name:                 "Archive",
		Provider:             models.StorageProviderS3,
		Configuration:        `{"bucket":"archive"}`,
		EncryptedCredentials: "encrypted-secret",
		Mounted:              false,
	}
	if err := db.Create(&mount).Error; err != nil {
		t.Fatal(err)
	}
	pool := models.StoragePool{UUID: "archive-pool", Name: "Archive"}
	if err := db.Create(&pool).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.StoragePoolMount{StoragePoolID: pool.ID, StorageMountID: mount.ID}).Error; err != nil {
		t.Fatal(err)
	}
	files := []models.File{
		{UUID: "available-file", StorageID: mount.UUID, StorageState: models.FileStorageAvailable, Size: 10},
		{UUID: "unavailable-file", StorageID: mount.UUID, StorageState: models.FileStorageUnavailable, Size: 20},
	}
	for index := range files {
		if err := db.Create(&files[index]).Error; err != nil {
			t.Fatal(err)
		}
	}

	service := NewService(&app.Deps{DB: db})
	unavailable, err := service.DeleteStorageMount(mount.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unavailable != 2 {
		t.Fatalf("unavailable files = %d, want 2", unavailable)
	}
	if err := db.Unscoped().First(&models.StorageMount{}, mount.ID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted mount lookup error = %v, want record not found", err)
	}
	var membershipCount int64
	if err := db.Model(&models.StoragePoolMount{}).Where("storage_mount_id = ?", mount.ID).Count(&membershipCount).Error; err != nil {
		t.Fatal(err)
	}
	if membershipCount != 0 {
		t.Fatalf("pool memberships = %d, want 0", membershipCount)
	}
	for index := range files {
		var persisted models.File
		if err := db.First(&persisted, files[index].ID).Error; err != nil {
			t.Fatal(err)
		}
		if persisted.StorageID != mount.UUID || persisted.StorageState != models.FileStorageUnavailable {
			t.Fatalf("persisted file = %#v", persisted)
		}
	}

	overview, err := service.StorageAdminOverview()
	if err != nil {
		t.Fatal(err)
	}
	if overview.FileCount != 2 || overview.UsedBytes != 30 || overview.UnavailableFileCount != 2 {
		t.Fatalf("overview totals = files:%d bytes:%d unavailable:%d", overview.FileCount, overview.UsedBytes, overview.UnavailableFileCount)
	}
	if len(overview.Mounts) != 0 {
		t.Fatalf("deleted mount still listed: %#v", overview.Mounts)
	}
}

func TestDeleteStorageMountRequiresDetachedNonSystemMount(t *testing.T) {
	tests := []struct {
		name  string
		mount models.StorageMount
		want  string
	}{
		{name: "mounted", mount: models.StorageMount{UUID: "mounted", Provider: models.StorageProviderS3, Mounted: true}, want: "detach"},
		{name: "system", mount: models.StorageMount{UUID: models.StorageMountLocalUUID, Provider: models.StorageProviderLocal, System: true}, want: "built-in"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newStorageAdminTestDB(t)
			if err := db.Create(&test.mount).Error; err != nil {
				t.Fatal(err)
			}
			service := NewService(&app.Deps{DB: db})
			if _, err := service.DeleteStorageMount(test.mount.ID); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DeleteStorageMount() error = %v, want %q", err, test.want)
			}
			if err := db.First(&models.StorageMount{}, test.mount.ID).Error; err != nil {
				t.Fatalf("protected mount was deleted: %v", err)
			}
		})
	}
}

func TestDeleteStoragePoolFallsBackToLocalAndClearsUserOverrides(t *testing.T) {
	db := newStorageAdminTestDB(t)
	localMount := models.StorageMount{UUID: models.StorageMountLocalUUID, Name: "Local", Provider: models.StorageProviderLocal, Mounted: true, System: true}
	remoteMount := models.StorageMount{UUID: "remote", Name: "Remote", Provider: models.StorageProviderS3, Mounted: true}
	if err := db.Create(&localMount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&remoteMount).Error; err != nil {
		t.Fatal(err)
	}
	localPool := models.StoragePool{UUID: models.StoragePoolLocalUUID, Name: "Local", System: true}
	if err := db.Create(&localPool).Error; err != nil {
		t.Fatal(err)
	}
	service := &Service{Deps: &app.Deps{DB: db}}
	remotePool, err := service.CreateStoragePool(StoragePoolInput{Name: "Remote", MountIDs: []uint{remoteMount.ID}, IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	user := models.User{Username: "user", StoragePoolID: &remotePool.ID}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteStoragePool(remotePool.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&localPool, localPool.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !localPool.IsDefault {
		t.Fatal("local pool was not restored as default")
	}
	if err := db.First(&user, user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if user.StoragePoolID != nil {
		t.Fatalf("user storage pool override = %v, want inherit", user.StoragePoolID)
	}
}

func TestStoragePoolAllowsBuiltInLocalMountAsDedicatedCache(t *testing.T) {
	db := newStorageAdminTestDB(t)
	localMount := models.StorageMount{
		UUID: models.StorageMountLocalUUID, Name: "Local storage", Provider: models.StorageProviderLocal, Mounted: true, System: true,
	}
	remoteMount := models.StorageMount{UUID: "remote", Name: "Remote", Provider: models.StorageProviderS3, Mounted: true}
	if err := db.Create(&localMount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&remoteMount).Error; err != nil {
		t.Fatal(err)
	}
	service := &Service{Deps: &app.Deps{DB: db}}
	pool, err := service.CreateStoragePool(StoragePoolInput{
		Name: "Remote with local cache", PrimaryMountIDs: []uint{remoteMount.ID},
		CacheMounts: []StorageCacheMountInput{{MountID: localMount.ID, MaxBytes: 50 * 1024 * 1024}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var memberships []models.StoragePoolMount
	if err := db.Where("storage_pool_id = ?", pool.ID).Order("storage_mount_id ASC").Find(&memberships).Error; err != nil {
		t.Fatal(err)
	}
	if len(memberships) != 2 {
		t.Fatalf("memberships=%#v", memberships)
	}
	roles := map[uint]models.StoragePoolMount{}
	for _, membership := range memberships {
		roles[membership.StorageMountID] = membership
	}
	if roles[remoteMount.ID].Role != models.StoragePoolMountPrimary {
		t.Fatalf("remote role=%q", roles[remoteMount.ID].Role)
	}
	if roles[localMount.ID].Role != models.StoragePoolMountCache || roles[localMount.ID].CacheMaxBytes != 50*1024*1024 {
		t.Fatalf("local cache membership=%#v", roles[localMount.ID])
	}
	overview, err := service.StorageAdminOverview()
	if err != nil {
		t.Fatal(err)
	}
	var response *StoragePoolResponse
	for index := range overview.Pools {
		if overview.Pools[index].ID == pool.ID {
			response = &overview.Pools[index]
			break
		}
	}
	if response == nil || len(response.PrimaryMountIDs) != 1 || response.PrimaryMountIDs[0] != remoteMount.ID {
		t.Fatalf("pool overview=%#v", response)
	}
	if len(response.CacheMounts) != 1 || response.CacheMounts[0].MountID != localMount.ID ||
		response.CacheMounts[0].MaxBytes != 50*1024*1024 || response.CacheMounts[0].MinimumFreePct != 10 {
		t.Fatalf("cache overview=%#v", response.CacheMounts)
	}
	_, err = service.UpdateStoragePool(pool.ID, StoragePoolInput{
		Name: pool.Name, PrimaryMountIDs: []uint{remoteMount.ID},
		CacheMounts: []StorageCacheMountInput{{MountID: remoteMount.ID, MaxBytes: 1}},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be primary") {
		t.Fatalf("same-role validation error=%v", err)
	}
}

func TestStorageAdminOverviewReportsRuntimeAvailability(t *testing.T) {
	db := newStorageAdminTestDB(t)
	localStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storageService, err := storage.NewService(models.StorageMountLocalUUID, storage.LegacyMediaLayout{}, map[string]storage.Store{
		models.StorageMountLocalUUID: localStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageService.Close() })
	mounts := []models.StorageMount{
		{UUID: models.StorageMountLocalUUID, Name: "Local", Provider: models.StorageProviderLocal, Mounted: true, System: true},
		{UUID: "missing-runtime", Name: "Missing", Provider: models.StorageProviderS3, Mounted: true},
		{UUID: "health-error", Name: "Error", Provider: models.StorageProviderS3, Mounted: true, LastError: "connection failed"},
		{
			UUID: "detached-sftp", Name: "SFTP", Provider: models.StorageProviderSFTP,
			Configuration: `{"host":"storage.example.com","port":22,"username":"media","root":"videocms","authentication":"password","host_key_fingerprints":["SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"]}`,
		},
	}
	for index := range mounts {
		if err := db.Create(&mounts[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(&app.Deps{DB: db, Storage: storageService})
	overview, err := service.StorageAdminOverview()
	if err != nil {
		t.Fatal(err)
	}
	availability := make(map[string]bool, len(overview.Mounts))
	var sftpConfiguration storage.SFTPMountConfiguration
	for _, mount := range overview.Mounts {
		availability[mount.UUID] = mount.Available
		if mount.UUID == "detached-sftp" {
			var ok bool
			sftpConfiguration, ok = mount.Configuration.(storage.SFTPMountConfiguration)
			if !ok {
				t.Fatalf("SFTP configuration type = %T", mount.Configuration)
			}
		}
	}
	if !availability[models.StorageMountLocalUUID] {
		t.Fatal("mounted local runtime store should be available")
	}
	if availability["missing-runtime"] || availability["health-error"] {
		t.Fatalf("unhealthy availability = %#v", availability)
	}
	if sftpConfiguration.Host != "storage.example.com" || sftpConfiguration.Root != "videocms" {
		t.Fatalf("SFTP configuration = %#v", sftpConfiguration)
	}
}

func TestStorageAdminOverviewAttributesRecentTrafficToPoolsAndServingMounts(t *testing.T) {
	db := newStorageAdminTestDB(t)
	origin := models.StorageMount{UUID: "remote-origin", Name: "Remote origin", Provider: models.StorageProviderS3, Mounted: true}
	cache := models.StorageMount{UUID: "local-cache", Name: "Local cache", Provider: models.StorageProviderLocal, Mounted: true}
	if err := db.Create(&origin).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&cache).Error; err != nil {
		t.Fatal(err)
	}
	pool := models.StoragePool{UUID: "traffic-pool", Name: "Traffic pool"}
	if err := db.Create(&pool).Error; err != nil {
		t.Fatal(err)
	}
	for _, membership := range []models.StoragePoolMount{
		{StoragePoolID: pool.ID, StorageMountID: origin.ID, Role: models.StoragePoolMountPrimary},
		{StoragePoolID: pool.ID, StorageMountID: cache.ID, Role: models.StoragePoolMountCache, CacheMaxBytes: 1024},
	} {
		if err := db.Create(&membership).Error; err != nil {
			t.Fatal(err)
		}
	}

	rows := []models.TrafficLog{
		{Source: models.TrafficSourcePlayer, Bytes: 100, StoragePoolID: pool.ID, StorageMountUUID: origin.UUID, DeliverySource: models.TrafficDeliverySourceOrigin},
		{Source: models.TrafficSourcePlayer, Bytes: 60, StoragePoolID: pool.ID, StorageMountUUID: cache.UUID, DeliverySource: models.TrafficDeliverySourceCache},
		{Source: models.TrafficSourcePlayer, Bytes: 999},
	}
	for index := range rows {
		if err := db.Create(&rows[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().UTC().AddDate(0, 0, -31)
	oldRow := models.TrafficLog{
		Source: models.TrafficSourcePlayer, Bytes: 500, StoragePoolID: pool.ID,
		StorageMountUUID: origin.UUID, DeliverySource: models.TrafficDeliverySourceOrigin,
	}
	if err := db.Create(&oldRow).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&oldRow).UpdateColumn("created_at", old).Error; err != nil {
		t.Fatal(err)
	}

	overview, err := NewService(&app.Deps{DB: db}).StorageAdminOverview()
	if err != nil {
		t.Fatal(err)
	}
	if overview.TrafficWindowDays != 30 {
		t.Fatalf("traffic window = %d, want 30", overview.TrafficWindowDays)
	}
	assertStorageTraffic(t, overview.Traffic, 160, 2, 100, 1, 60, 1)
	if len(overview.Pools) != 1 {
		t.Fatalf("pools = %#v", overview.Pools)
	}
	assertStorageTraffic(t, overview.Pools[0].Traffic, 160, 2, 100, 1, 60, 1)

	mountTraffic := make(map[string]StorageTrafficSummary)
	for _, mount := range overview.Mounts {
		mountTraffic[mount.UUID] = mount.Traffic
	}
	assertStorageTraffic(t, mountTraffic[origin.UUID], 100, 1, 100, 1, 0, 0)
	assertStorageTraffic(t, mountTraffic[cache.UUID], 60, 1, 0, 0, 60, 1)
}

func assertStorageTraffic(
	t *testing.T,
	got StorageTrafficSummary,
	bytes, requests, originBytes, originRequests, cacheBytes, cacheRequests uint64,
) {
	t.Helper()
	want := StorageTrafficSummary{
		Bytes: bytes, Requests: requests, OriginBytes: originBytes, OriginRequests: originRequests,
		CacheBytes: cacheBytes, CacheRequests: cacheRequests,
	}
	if got != want {
		t.Fatalf("storage traffic = %#v, want %#v", got, want)
	}
}

func TestUpdatingDefaultStoragePoolWithoutDefaultFallsBackToLocal(t *testing.T) {
	db := newStorageAdminTestDB(t)
	localMount := models.StorageMount{UUID: models.StorageMountLocalUUID, Name: "Local", Provider: models.StorageProviderLocal, Mounted: true, System: true}
	remoteMount := models.StorageMount{UUID: "remote", Name: "Remote", Provider: models.StorageProviderS3, Mounted: true}
	if err := db.Create(&localMount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&remoteMount).Error; err != nil {
		t.Fatal(err)
	}
	localPool := models.StoragePool{UUID: models.StoragePoolLocalUUID, Name: "Local", System: true}
	if err := db.Create(&localPool).Error; err != nil {
		t.Fatal(err)
	}
	service := &Service{Deps: &app.Deps{DB: db}}
	remotePool, err := service.CreateStoragePool(StoragePoolInput{Name: "Remote", MountIDs: []uint{remoteMount.ID}, IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateStoragePool(remotePool.ID, StoragePoolInput{Name: "Remote", MountIDs: []uint{remoteMount.ID}, IsDefault: false}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&localPool, localPool.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !localPool.IsDefault {
		t.Fatal("local pool was not restored as default")
	}
	var defaultCount int64
	if err := db.Model(&models.StoragePool{}).Where("is_default = ?", true).Count(&defaultCount).Error; err != nil {
		t.Fatal(err)
	}
	if defaultCount != 1 {
		t.Fatalf("default pool count = %d, want 1", defaultCount)
	}
}

func TestMountedStorageLocationMustBeDetachedBeforeEdit(t *testing.T) {
	db := newStorageAdminTestDB(t)
	encodedKey := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	cipher, err := storage.NewCredentialCipher(encodedKey)
	if err != nil {
		t.Fatal(err)
	}
	mount := models.StorageMount{
		UUID:     "remote",
		Name:     "Remote",
		Provider: models.StorageProviderS3,
		Mounted:  true,
	}
	mount.Configuration, mount.EncryptedCredentials, err = storage.EncodeS3Mount(
		storage.S3MountConfiguration{Bucket: "media", Region: "us-east-1"},
		storage.S3MountCredentials{AccessKeyID: "access", SecretAccessKey: "secret"},
		mount.UUID,
		cipher,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&mount).Error; err != nil {
		t.Fatal(err)
	}
	localStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remoteStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storageService, err := storage.NewService(models.StorageMountLocalUUID, storage.LegacyMediaLayout{}, map[string]storage.Store{
		models.StorageMountLocalUUID: localStore,
		mount.UUID:                   remoteStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageService.Close() })
	service := NewService(&app.Deps{DB: db, Storage: storageService, StorageCipher: cipher})

	_, err = service.UpdateS3StorageMount(context.Background(), mount.ID, S3StorageMountInput{
		Name:          mount.Name,
		Configuration: storage.S3MountConfiguration{Bucket: "replacement", Region: "us-east-1"},
	})
	if err == nil || !strings.Contains(err.Error(), "detach") {
		t.Fatalf("UpdateS3StorageMount() error = %v, want detach requirement", err)
	}
	var persisted models.StorageMount
	if err := db.First(&persisted, mount.ID).Error; err != nil {
		t.Fatal(err)
	}
	configuration, err := storage.DecodeS3MountConfiguration(persisted.Configuration)
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Bucket != "media" {
		t.Fatalf("mounted bucket changed to %q", configuration.Bucket)
	}
}

func TestUnmountWaitsForUploadPlacementLease(t *testing.T) {
	db := newStorageAdminTestDB(t)
	localStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remoteStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mount := models.StorageMount{UUID: "remote", Name: "Remote", Provider: models.StorageProviderS3, Mounted: true}
	if err := db.Create(&mount).Error; err != nil {
		t.Fatal(err)
	}
	pool := models.StoragePool{UUID: "remote-pool", Name: "Remote", IsDefault: true}
	if err := db.Create(&pool).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.StoragePoolMount{StoragePoolID: pool.ID, StorageMountID: mount.ID}).Error; err != nil {
		t.Fatal(err)
	}
	user := models.User{Username: "uploader"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	storageService, err := storage.NewService(models.StorageMountLocalUUID, storage.LegacyMediaLayout{}, map[string]storage.Store{
		models.StorageMountLocalUUID: localStore,
		mount.UUID:                   remoteStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageService.Close() })
	service := NewService(&app.Deps{DB: db, Storage: storageService})
	sourcePath := filepath.Join(t.TempDir(), "source.mp4")
	if err := os.WriteFile(sourcePath, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceKey, err := storageService.Layout().Source("new-file", "original.mp4")
	if err != nil {
		t.Fatal(err)
	}
	storeID, release, err := service.publishUploadSource(context.Background(), user.ID, sourceKey, sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if storeID != mount.UUID {
		t.Fatalf("selected store = %q, want %q", storeID, mount.UUID)
	}
	file := models.File{UUID: "new-file", StorageID: mount.UUID, StorageState: models.FileStorageAvailable}
	if err := db.Create(&file).Error; err != nil {
		release()
		t.Fatal(err)
	}

	type unmountResult struct {
		unavailable int64
		err         error
	}
	result := make(chan unmountResult, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		unavailable, err := service.UnmountStorageMount(mount.ID)
		result <- unmountResult{unavailable: unavailable, err: err}
	}()
	<-started
	select {
	case early := <-result:
		release()
		t.Fatalf("unmount completed before upload lease release: %#v", early)
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case got := <-result:
		if got.err != nil || got.unavailable != 1 {
			t.Fatalf("unmount result = %#v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("unmount did not resume after upload lease release")
	}
	if err := db.First(&file, file.ID).Error; err != nil {
		t.Fatal(err)
	}
	if file.StorageState != models.FileStorageUnavailable {
		t.Fatalf("file storage state = %q, want unavailable", file.StorageState)
	}
}

func TestReconnectRequiresPersistedObjectAnchors(t *testing.T) {
	db := newStorageAdminTestDB(t)
	localStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remoteStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	storageService, err := storage.NewService(models.StorageMountLocalUUID, storage.LegacyMediaLayout{}, map[string]storage.Store{
		models.StorageMountLocalUUID: localStore,
		"remote":                     remoteStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageService.Close() })
	mount := models.StorageMount{UUID: "remote", Name: "Remote", Provider: models.StorageProviderS3, Mounted: true}
	if err := db.Create(&mount).Error; err != nil {
		t.Fatal(err)
	}
	sourceKey, err := storageService.Layout().Source("anchored-file", "original.mp4")
	if err != nil {
		t.Fatal(err)
	}
	file := models.File{UUID: "anchored-file", StorageID: "old", StorageState: models.FileStorageUnavailable, SourceKey: sourceKey.String()}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	strayKey, err := storageService.Layout().Thumbnail(file.UUID, "4x4.webp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remoteStore.Put(context.Background(), strayKey, strings.NewReader("stray"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	service := NewService(&app.Deps{DB: db, Storage: storageService})
	preview, err := service.ReconnectStorageMount(context.Background(), mount.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Matched != 0 {
		t.Fatalf("stray prefix matched file: %#v", preview)
	}
	if _, err := remoteStore.Put(context.Background(), sourceKey, strings.NewReader("source"), storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	preview, err = service.ReconnectStorageMount(context.Background(), mount.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Matched != 1 {
		t.Fatalf("persisted source anchor did not match: %#v", preview)
	}
}

func TestReconnectUsesBoundedConcurrency(t *testing.T) {
	db := newStorageAdminTestDB(t)
	localStore, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	baseRemote, err := storage.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remoteStore := &delayedStatStore{Store: baseRemote}
	storageService, err := storage.NewService(models.StorageMountLocalUUID, storage.LegacyMediaLayout{}, map[string]storage.Store{
		models.StorageMountLocalUUID: localStore,
		"remote":                     remoteStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storageService.Close() })
	mount := models.StorageMount{UUID: "remote", Name: "Remote", Provider: models.StorageProviderS3, Mounted: true}
	if err := db.Create(&mount).Error; err != nil {
		t.Fatal(err)
	}
	fileCount := storageReconnectFileBatchSize*2 + 5
	for index := range fileCount {
		fileUUID := fmt.Sprintf("file-%02d", index)
		sourceKey, err := storageService.Layout().Source(fileUUID, "original.mp4")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.File{
			UUID: fileUUID, StorageID: "old", StorageState: models.FileStorageUnavailable, SourceKey: sourceKey.String(),
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(&app.Deps{DB: db, Storage: storageService})
	preview, err := service.ReconnectStorageMount(context.Background(), mount.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Scanned != fileCount || preview.Matched != fileCount {
		t.Fatalf("preview = %#v", preview)
	}
	if remoteStore.maxActive <= 1 || remoteStore.maxActive > storageReconnectConcurrency {
		t.Fatalf("maximum concurrent scans = %d, want 2..%d", remoteStore.maxActive, storageReconnectConcurrency)
	}
}

type delayedStatStore struct {
	storage.Store
	mu        sync.Mutex
	active    int
	maxActive int
}

type adminCloseTrackingStore struct {
	storage.Store
	mu         sync.Mutex
	closeCount int
}

func (s *adminCloseTrackingStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeCount++
	return nil
}

func (s *adminCloseTrackingStore) CloseCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeCount
}

func (s *delayedStatStore) Stat(ctx context.Context, key storage.Key) (storage.ObjectInfo, error) {
	s.mu.Lock()
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.active--
		s.mu.Unlock()
	}()
	timer := time.NewTimer(10 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return storage.ObjectInfo{}, ctx.Err()
	case <-timer.C:
		return storage.ObjectInfo{Key: key, Size: 1}, nil
	}
}

func newStorageAdminTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.StorageMount{},
		&models.StoragePool{},
		&models.StoragePoolMount{},
		&models.User{},
		&models.File{},
		&models.Quality{},
		&models.Audio{},
		&models.Subtitle{},
		&models.Link{},
		&models.TrafficLog{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}
