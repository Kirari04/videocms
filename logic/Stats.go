package logic

import (
	"ch/kirari04/videocms/inits"
	"math"
	"time"
)

type StatPoint struct {
	Timestamp int64
	Value     float64
}

type SystemStatsData struct {
	Cpu              []StatPoint
	Mem              []StatPoint
	NetOut           []StatPoint
	NetIn            []StatPoint
	DiskW            []StatPoint
	DiskR            []StatPoint
	ENCQualityQueue  []StatPoint
	ENCAudioQueue    []StatPoint
	ENCSubtitleQueue []StatPoint
}

type aggregatedResult struct {
	Ts               int64
	Cpu              float64
	Mem              float64
	NetOut           float64
	NetIn            float64
	DiskW            float64
	DiskR            float64
	ENCQualityQueue  float64
	ENCAudioQueue    float64
	ENCSubtitleQueue float64
}

func GetSystemStats(from time.Time, to time.Time, points int) (SystemStatsData, error) {
	result := SystemStatsData{
		Cpu:              make([]StatPoint, 0),
		Mem:              make([]StatPoint, 0),
		NetOut:           make([]StatPoint, 0),
		NetIn:            make([]StatPoint, 0),
		DiskW:            make([]StatPoint, 0),
		DiskR:            make([]StatPoint, 0),
		ENCQualityQueue:  make([]StatPoint, 0),
		ENCAudioQueue:    make([]StatPoint, 0),
		ENCSubtitleQueue: make([]StatPoint, 0),
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

	var aggregations []aggregatedResult

	// SQLite-specific optimization: Group by calculated time bucket
	// (strftime('%s', created_at) / step) * step
	if err := inits.DB.Table("system_resources").
		Select(`
			(CAST(strftime('%s', created_at) AS INTEGER) / ? ) * ? as ts,
			AVG(cpu) as cpu,
			AVG(mem) as mem,
			AVG(net_out) as net_out,
			AVG(net_in) as net_in,
			AVG(disk_w) as disk_w,
			AVG(disk_r) as disk_r,
			AVG(enc_quality_queue) as enc_quality_queue,
			AVG(enc_audio_queue) as enc_audio_queue,
			AVG(enc_subtitle_queue) as enc_subtitle_queue
		`, stepSeconds, stepSeconds).
		Where("created_at >= ? AND created_at <= ?", from, to).
		Group("ts").
		Order("ts asc").
		Scan(&aggregations).Error; err != nil {
		return result, err
	}

	// Convert aggregations to map for O(1) lookup
	aggMap := make(map[int64]aggregatedResult)
	for _, agg := range aggregations {
		aggMap[agg.Ts] = agg
	}

	// Iterate through all buckets to fill gaps with zeros
	startTs := from.Unix()
	endTs := to.Unix()

	// Align startTs to the grid
	startTs = (startTs / stepSeconds) * stepSeconds

	for ts := startTs; ts <= endTs; ts += stepSeconds {
		pointTs := ts * 1000 // Convert to Milliseconds for ApexCharts

		if val, ok := aggMap[ts]; ok {
			result.Cpu = append(result.Cpu, StatPoint{pointTs, val.Cpu})
			result.Mem = append(result.Mem, StatPoint{pointTs, val.Mem})
			result.NetOut = append(result.NetOut, StatPoint{pointTs, val.NetOut})
			result.NetIn = append(result.NetIn, StatPoint{pointTs, val.NetIn})
			result.DiskW = append(result.DiskW, StatPoint{pointTs, val.DiskW})
			result.DiskR = append(result.DiskR, StatPoint{pointTs, val.DiskR})
			result.ENCQualityQueue = append(result.ENCQualityQueue, StatPoint{pointTs, val.ENCQualityQueue})
			result.ENCAudioQueue = append(result.ENCAudioQueue, StatPoint{pointTs, val.ENCAudioQueue})
			result.ENCSubtitleQueue = append(result.ENCSubtitleQueue, StatPoint{pointTs, val.ENCSubtitleQueue})
		} else {
			// Fill with zeros
			result.Cpu = append(result.Cpu, StatPoint{pointTs, 0})
			result.Mem = append(result.Mem, StatPoint{pointTs, 0})
			result.NetOut = append(result.NetOut, StatPoint{pointTs, 0})
			result.NetIn = append(result.NetIn, StatPoint{pointTs, 0})
			result.DiskW = append(result.DiskW, StatPoint{pointTs, 0})
			result.DiskR = append(result.DiskR, StatPoint{pointTs, 0})
			result.ENCQualityQueue = append(result.ENCQualityQueue, StatPoint{pointTs, 0})
			result.ENCAudioQueue = append(result.ENCAudioQueue, StatPoint{pointTs, 0})
			result.ENCSubtitleQueue = append(result.ENCSubtitleQueue, StatPoint{pointTs, 0})
		}
	}

	return result, nil
}