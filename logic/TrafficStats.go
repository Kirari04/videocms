package logic

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"math"
	"time"
)

type TrafficStatPoint struct {
	Timestamp int64
	Bytes     uint64
}

type TrafficStatsData struct {
	Traffic []TrafficStatPoint
}

type aggregatedTrafficResult struct {
	Ts    int64   `gorm:"column:ts"`
	Bytes float64 `gorm:"column:bytes"`
}

func GetTrafficStats(from time.Time, to time.Time, points int, userID uint, fileID uint, qualityID uint) (TrafficStatsData, error) {
	result := TrafficStatsData{
		Traffic: make([]TrafficStatPoint, 0),
	}

	duration := to.Sub(from)
	if duration <= 0 || points <= 0 {
		return result, nil
	}

	// Calculate step in seconds
	stepSeconds := int64(math.Ceil(duration.Seconds() / float64(points)))
	if stepSeconds < 1 {
		stepSeconds = 1
	}

	var aggregations []aggregatedTrafficResult

	// Use strftime('%s') for comparison to handle timezone offsets correctly in SQLite
	query := inits.DB.Model(&models.TrafficLog{}).
		Select(`
			(CAST(strftime('%s', created_at) AS INTEGER) / ? ) * ? as ts,
			CAST(SUM(bytes) AS INTEGER) as bytes
		`, stepSeconds, stepSeconds).
		Where("CAST(strftime('%s', created_at) AS INTEGER) >= ? AND CAST(strftime('%s', created_at) AS INTEGER) <= ?", from.Unix(), to.Unix())

	if userID != 0 {
		query = query.Where("user_id = ?", userID)
	}
	if fileID != 0 {
		query = query.Where("file_id = ?", fileID)
	}
	if qualityID != 0 {
		query = query.Where("quality_id = ?", qualityID)
	}

	if err := query.Group("ts").
		Order("ts asc").
		Scan(&aggregations).Error; err != nil {
		return result, err
	}

	// Convert aggregations to map for O(1) lookup
	aggMap := make(map[int64]uint64)
	for _, agg := range aggregations {
		aggMap[agg.Ts] = uint64(agg.Bytes)
	}

	// Iterate through all buckets to fill gaps with zeros
	startTs := from.Unix()
	endTs := to.Unix()

	// Align startTs to the grid
	startTs = (startTs / stepSeconds) * stepSeconds

	for ts := startTs; ts <= endTs; ts += stepSeconds {
		pointTs := ts * 1000 // Convert to Milliseconds for ApexCharts

		if val, ok := aggMap[ts]; ok {
			result.Traffic = append(result.Traffic, TrafficStatPoint{pointTs, val})
		} else {
			result.Traffic = append(result.Traffic, TrafficStatPoint{pointTs, 0})
		}
	}

	return result, nil
}

func GetUploadStats(from time.Time, to time.Time, points int, userID uint) (TrafficStatsData, error) {
	result := TrafficStatsData{
		Traffic: make([]TrafficStatPoint, 0),
	}

	duration := to.Sub(from)
	if duration <= 0 || points <= 0 {
		return result, nil
	}

	// Calculate step in seconds
	stepSeconds := int64(math.Ceil(duration.Seconds() / float64(points)))
	if stepSeconds < 1 {
		stepSeconds = 1
	}

	var aggregations []aggregatedTrafficResult

	// Use strftime('%s') for comparison to handle timezone offsets correctly in SQLite
	query := inits.DB.Model(&models.UploadLog{}).
		Select(`
			(CAST(strftime('%s', created_at) AS INTEGER) / ? ) * ? as ts,
			CAST(SUM(bytes) AS INTEGER) as bytes
		`, stepSeconds, stepSeconds).
		Where("CAST(strftime('%s', created_at) AS INTEGER) >= ? AND CAST(strftime('%s', created_at) AS INTEGER) <= ?", from.Unix(), to.Unix())

	if userID != 0 {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.Group("ts").
		Order("ts asc").
		Scan(&aggregations).Error; err != nil {
		return result, err
	}

	// Convert aggregations to map for O(1) lookup
	aggMap := make(map[int64]uint64)
	for _, agg := range aggregations {
		aggMap[agg.Ts] = uint64(agg.Bytes)
	}

	// Iterate through all buckets to fill gaps with zeros
	startTs := from.Unix()
	endTs := to.Unix()

	// Align startTs to the grid
	startTs = (startTs / stepSeconds) * stepSeconds

	for ts := startTs; ts <= endTs; ts += stepSeconds {
		pointTs := ts * 1000 // Convert to Milliseconds for ApexCharts

		if val, ok := aggMap[ts]; ok {
			result.Traffic = append(result.Traffic, TrafficStatPoint{pointTs, val})
		} else {
			result.Traffic = append(result.Traffic, TrafficStatPoint{pointTs, 0})
		}
	}

	return result, nil
}

type aggregatedEncodingResult struct {
	Ts      int64   `gorm:"column:ts"`
	Seconds float64 `gorm:"column:seconds"`
}

func GetEncodingStats(from time.Time, to time.Time, points int, userID uint) (TrafficStatsData, error) {
	result := TrafficStatsData{
		Traffic: make([]TrafficStatPoint, 0),
	}

	duration := to.Sub(from)
	if duration <= 0 || points <= 0 {
		return result, nil
	}

	// Calculate step in seconds
	stepSeconds := int64(math.Ceil(duration.Seconds() / float64(points)))
	if stepSeconds < 1 {
		stepSeconds = 1
	}

	var aggregations []aggregatedEncodingResult

	query := inits.DB.Model(&models.EncodingLog{}).
		Select(`
			(CAST(strftime('%s', created_at) AS INTEGER) / ? ) * ? as ts,
			SUM(seconds) as seconds
		`, stepSeconds, stepSeconds).
		Where("CAST(strftime('%s', created_at) AS INTEGER) >= ? AND CAST(strftime('%s', created_at) AS INTEGER) <= ?", from.Unix(), to.Unix())

	if userID != 0 {
		query = query.Where("user_id = ?", userID)
	}

	if err := query.Group("ts").
		Order("ts asc").
		Scan(&aggregations).Error; err != nil {
		return result, err
	}

	aggMap := make(map[int64]uint64)
	for _, agg := range aggregations {
		aggMap[agg.Ts] = uint64(agg.Seconds)
	}

	startTs := from.Unix()
	endTs := to.Unix()
	startTs = (startTs / stepSeconds) * stepSeconds

	for ts := startTs; ts <= endTs; ts += stepSeconds {
		pointTs := ts * 1000

		if val, ok := aggMap[ts]; ok {
			result.Traffic = append(result.Traffic, TrafficStatPoint{pointTs, val})
		} else {
			result.Traffic = append(result.Traffic, TrafficStatPoint{pointTs, 0})
		}
	}

	return result, nil
}

type TopTrafficResult struct {
	ID    uint   `gorm:"column:id"`
	Name  string `gorm:"-"`
	Bytes uint64 `gorm:"column:bytes"`
}

func GetTopTraffic(from time.Time, to time.Time, userID uint, limit int, mode string) ([]TopTrafficResult, error) {
	var results []TopTrafficResult

	query := inits.DB.Model(&models.TrafficLog{}).
		Where("CAST(strftime('%s', created_at) AS INTEGER) >= ? AND CAST(strftime('%s', created_at) AS INTEGER) <= ?", from.Unix(), to.Unix())

	switch mode {
	case "files":
		// Rank files
		selectStr := "file_id as id, CAST(SUM(bytes) AS INTEGER) as bytes"
		if userID != 0 {
			query = query.Where("user_id = ?", userID)
		}
		
		// Join with links or files to get a name
		// We use links as they are the public representation
		err := query.Select(selectStr).
			Group("file_id").
			Order("bytes DESC").
			Limit(limit).
			Scan(&results).Error
		
		if err != nil {
			return nil, err
		}

		// Populate names from Link table (best effort)
		for i := range results {
			var link models.Link
			if err := inits.DB.Where("file_id = ?", results[i].ID).First(&link).Error; err == nil {
				results[i].Name = link.Name
			} else {
				results[i].Name = "Unknown File"
			}
		}

	case "users":
		// Rank users (Admin only context usually)
		err := query.Select("user_id as id, CAST(SUM(bytes) AS INTEGER) as bytes").
			Group("user_id").
			Order("bytes DESC").
			Limit(limit).
			Scan(&results).Error
		
		if err != nil {
			return nil, err
		}

		// Populate names from User table
		for i := range results {
			var user models.User
			if err := inits.DB.First(&user, results[i].ID).Error; err == nil {
				results[i].Name = user.Username
			} else {
				results[i].Name = "Deleted User"
			}
		}
	}

	return results, nil
}
