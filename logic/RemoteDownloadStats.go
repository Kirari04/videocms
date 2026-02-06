package logic

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"math"
	"time"
)

func GetRemoteDownloadStats(from time.Time, to time.Time, points int, userID uint) (TrafficStatsData, error) {
	result := TrafficStatsData{
		Traffic: make([]TrafficStatPoint, 0),
	}

	duration := to.Sub(from)
	if duration <= 0 || points <= 0 {
		return result, nil
	}

	stepSeconds := int64(math.Ceil(duration.Seconds() / float64(points)))
	if stepSeconds < 1 {
		stepSeconds = 1
	}

	var aggregations []aggregatedTrafficResult

	query := inits.DB.Model(&models.RemoteDownloadLog{}).
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

	aggMap := make(map[int64]uint64)
	for _, agg := range aggregations {
		aggMap[agg.Ts] = uint64(agg.Bytes)
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

func GetRemoteDownloadDurationStats(from time.Time, to time.Time, points int, userID uint) (TrafficStatsData, error) {
	result := TrafficStatsData{
		Traffic: make([]TrafficStatPoint, 0),
	}

	duration := to.Sub(from)
	if duration <= 0 || points <= 0 {
		return result, nil
	}

	stepSeconds := int64(math.Ceil(duration.Seconds() / float64(points)))
	if stepSeconds < 1 {
		stepSeconds = 1
	}

	var aggregations []aggregatedEncodingResult

	query := inits.DB.Model(&models.RemoteDownloadLog{}).
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

func GetTopRemoteDownloadTraffic(from time.Time, to time.Time, userID uint, limit int, mode string) ([]TopTrafficResult, error) {
	var results []TopTrafficResult

	query := inits.DB.Model(&models.RemoteDownloadLog{}).
		Where("CAST(strftime('%s', remote_download_logs.created_at) AS INTEGER) >= ? AND CAST(strftime('%s', remote_download_logs.created_at) AS INTEGER) <= ?", from.Unix(), to.Unix())

	switch mode {
	case "domains":
		if userID != 0 {
			query = query.Where("user_id = ?", userID)
		}
		err := query.Select("0 as id, CASE WHEN domain = '' OR domain IS NULL THEN 'Unknown' ELSE domain END as name, CAST(SUM(bytes) AS INTEGER) as value").
			Group("name").
			Order("value DESC").
			Limit(limit).
			Scan(&results).Error

		if err != nil {
			return nil, err
		}

	case "users":
		if userID != 0 {
			query = query.Where("remote_download_logs.user_id = ?", userID)
		}
		err := query.Joins("INNER JOIN users ON users.id = remote_download_logs.user_id").
			Select("remote_download_logs.user_id as id, users.username as name, CAST(SUM(bytes) AS INTEGER) as value").
			Group("remote_download_logs.user_id, users.username").
			Order("value DESC").
			Limit(limit).
			Scan(&results).Error

		if err != nil {
			return nil, err
		}

	case "duration":
		if userID != 0 {
			query = query.Where("remote_download_logs.user_id = ?", userID)
		}
		err := query.Joins("INNER JOIN users ON users.id = remote_download_logs.user_id").
			Select("remote_download_logs.user_id as id, users.username as name, CAST(SUM(seconds) AS INTEGER) as value").
			Group("remote_download_logs.user_id, users.username").
			Order("value DESC").
			Limit(limit).
			Scan(&results).Error

		if err != nil {
			return nil, err
		}
	}

	return results, nil
}
