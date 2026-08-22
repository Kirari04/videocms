package logic

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"ch/kirari04/videocms/models"
	trafficrecorder "ch/kirari04/videocms/traffic"
)

const maxStorageDeliveryPoints = 200

// StorageDeliveryStatPoint is one chart bucket. Timestamps are milliseconds
// since epoch to match the other stats endpoints consumed by ApexCharts.
type StorageDeliveryStatPoint struct {
	Timestamp      int64
	OriginBytes    uint64
	OriginRequests uint64
	CacheBytes     uint64
	CacheRequests  uint64
}

type StoragePoolTrafficBreakdown struct {
	ID      uint
	Name    string
	Traffic StorageTrafficSummary
}

type StorageMountTrafficBreakdown struct {
	UUID     string
	Name     string
	Provider string
	Traffic  StorageTrafficSummary
}

type StorageDeliveryStats struct {
	Traffic         []StorageDeliveryStatPoint
	Summary         StorageTrafficSummary
	Pools           []StoragePoolTrafficBreakdown
	Mounts          []StorageMountTrafficBreakdown
	CacheConfigured bool
	TrafficRecorder trafficrecorder.Status
}

type storageDeliveryTimeAggregate struct {
	Timestamp      int64  `gorm:"column:timestamp"`
	DeliverySource string `gorm:"column:delivery_source"`
	Bytes          uint64 `gorm:"column:bytes"`
	Requests       uint64 `gorm:"column:requests"`
}

// GetStorageDeliveryStats returns the time series and exact storage
// attribution used by the admin system-stats page. It deliberately excludes
// legacy traffic rows that were recorded before storage attribution existed.
func (s *Service) GetStorageDeliveryStats(from time.Time, to time.Time, points int) (StorageDeliveryStats, error) {
	result := StorageDeliveryStats{
		Traffic: make([]StorageDeliveryStatPoint, 0),
		Pools:   make([]StoragePoolTrafficBreakdown, 0),
		Mounts:  make([]StorageMountTrafficBreakdown, 0),
	}
	if s == nil || s.Deps == nil || s.Deps.DB == nil {
		return result, errors.New("storage delivery statistics are not configured")
	}
	if s.Deps.Traffic != nil {
		result.TrafficRecorder = s.Deps.Traffic.Status()
	}
	var cacheCount int64
	if err := s.Deps.DB.Model(&models.StoragePoolMount{}).
		Where("role = ?", models.StoragePoolMountCache).
		Count(&cacheCount).Error; err != nil {
		return result, err
	}
	result.CacheConfigured = cacheCount > 0

	duration := to.Sub(from)
	if duration <= 0 || points <= 0 {
		return result, nil
	}
	if points > maxStorageDeliveryPoints {
		points = maxStorageDeliveryPoints
	}
	stepSeconds := int64(math.Ceil(duration.Seconds() / float64(points)))
	if stepSeconds < 60 {
		stepSeconds = 60
	}
	fromUnix := from.UTC().Truncate(time.Minute).Unix()
	toUnix := to.UTC().Unix()
	deliverySources := []string{models.TrafficDeliverySourceOrigin, models.TrafficDeliverySourceCache}

	var timeRows []storageDeliveryTimeAggregate
	if err := s.Deps.DB.Model(&models.TrafficLog{}).
		Select(`(bucket_start / ?) * ? AS timestamp, delivery_source,
			COALESCE(SUM(bytes), 0) AS bytes, COALESCE(SUM(request_count), 0) AS requests`, stepSeconds, stepSeconds).
		Where("bucket_start >= ? AND bucket_start <= ? AND delivery_source IN ?", fromUnix, toUnix, deliverySources).
		Group("timestamp, delivery_source").
		Order("timestamp ASC, delivery_source ASC").
		Scan(&timeRows).Error; err != nil {
		return result, err
	}

	byTimestamp := make(map[int64]StorageDeliveryStatPoint, len(timeRows))
	for _, row := range timeRows {
		point := byTimestamp[row.Timestamp]
		point.Timestamp = row.Timestamp * 1000
		if row.DeliverySource == models.TrafficDeliverySourceCache {
			point.CacheBytes += row.Bytes
			point.CacheRequests += row.Requests
			result.Summary.CacheBytes += row.Bytes
			result.Summary.CacheRequests += row.Requests
		} else {
			point.OriginBytes += row.Bytes
			point.OriginRequests += row.Requests
			result.Summary.OriginBytes += row.Bytes
			result.Summary.OriginRequests += row.Requests
		}
		result.Summary.Bytes += row.Bytes
		result.Summary.Requests += row.Requests
		byTimestamp[row.Timestamp] = point
	}

	startTimestamp := (fromUnix / stepSeconds) * stepSeconds
	for timestamp := startTimestamp; timestamp <= toUnix; timestamp += stepSeconds {
		point, ok := byTimestamp[timestamp]
		if !ok {
			point.Timestamp = timestamp * 1000
		}
		result.Traffic = append(result.Traffic, point)
	}

	breakdownRows, err := s.storageDeliveryBreakdowns(fromUnix, toUnix)
	if err != nil {
		return result, err
	}
	var pools []models.StoragePool
	if err := s.Deps.DB.Select("id", "name").Find(&pools).Error; err != nil {
		return result, err
	}
	poolNames := make(map[uint]string, len(pools))
	for _, pool := range pools {
		poolNames[pool.ID] = pool.Name
	}
	for id, traffic := range breakdownRows.byPool {
		name := poolNames[id]
		if name == "" {
			name = fmt.Sprintf("Removed pool #%d", id)
		}
		result.Pools = append(result.Pools, StoragePoolTrafficBreakdown{ID: id, Name: name, Traffic: traffic})
	}
	sort.Slice(result.Pools, func(i, j int) bool {
		if result.Pools[i].Traffic.Bytes == result.Pools[j].Traffic.Bytes {
			return result.Pools[i].Name < result.Pools[j].Name
		}
		return result.Pools[i].Traffic.Bytes > result.Pools[j].Traffic.Bytes
	})

	var mounts []models.StorageMount
	if err := s.Deps.DB.Select("uuid", "name", "provider").Find(&mounts).Error; err != nil {
		return result, err
	}
	mountByUUID := make(map[string]models.StorageMount, len(mounts))
	for _, mount := range mounts {
		mountByUUID[mount.UUID] = mount
	}
	for uuid, traffic := range breakdownRows.byMount {
		mount, ok := mountByUUID[uuid]
		name := mount.Name
		provider := mount.Provider
		if !ok {
			name = "Removed mount"
		}
		result.Mounts = append(result.Mounts, StorageMountTrafficBreakdown{
			UUID: uuid, Name: name, Provider: provider, Traffic: traffic,
		})
	}
	sort.Slice(result.Mounts, func(i, j int) bool {
		if result.Mounts[i].Traffic.Bytes == result.Mounts[j].Traffic.Bytes {
			return result.Mounts[i].Name < result.Mounts[j].Name
		}
		return result.Mounts[i].Traffic.Bytes > result.Mounts[j].Traffic.Bytes
	})

	return result, nil
}

type storageDeliveryBreakdownRows struct {
	byPool  map[uint]StorageTrafficSummary
	byMount map[string]StorageTrafficSummary
}

func (s *Service) storageDeliveryBreakdowns(fromUnix, toUnix int64) (storageDeliveryBreakdownRows, error) {
	rows := storageDeliveryBreakdownRows{
		byPool:  make(map[uint]StorageTrafficSummary),
		byMount: make(map[string]StorageTrafficSummary),
	}
	var aggregates []storageTrafficAggregate
	if err := s.Deps.DB.Model(&models.TrafficLog{}).
		Select(`storage_pool_id, storage_mount_uuid, delivery_source, COALESCE(SUM(bytes), 0) AS bytes,
			COALESCE(SUM(request_count), 0) AS requests`).
		Where("bucket_start >= ? AND bucket_start <= ? AND delivery_source IN ?", fromUnix, toUnix, []string{
			models.TrafficDeliverySourceOrigin,
			models.TrafficDeliverySourceCache,
		}).
		Where("storage_pool_id <> 0 OR storage_mount_uuid <> ''").
		Group("storage_pool_id, storage_mount_uuid, delivery_source").
		Scan(&aggregates).Error; err != nil {
		return rows, err
	}
	for _, aggregate := range aggregates {
		if aggregate.StoragePoolID != 0 {
			summary := rows.byPool[aggregate.StoragePoolID]
			addStorageTraffic(&summary, aggregate)
			rows.byPool[aggregate.StoragePoolID] = summary
		}
		if aggregate.StorageMountUUID != "" {
			summary := rows.byMount[aggregate.StorageMountUUID]
			addStorageTraffic(&summary, aggregate)
			rows.byMount[aggregate.StorageMountUUID] = summary
		}
	}
	return rows, nil
}
