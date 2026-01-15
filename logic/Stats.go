package logic

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
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

func GetSystemStats(from time.Time, to time.Time, points int) (SystemStatsData, error) {
	var resources []models.SystemResource
	
	// Fetch all data in range
	if res := inits.DB.
		Where("created_at >= ?", from).
		Where("created_at <= ?", to).
		Order("created_at asc").
		Find(&resources); res.Error != nil {
		return SystemStatsData{}, res.Error
	}

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

	if len(resources) == 0 {
		return result, nil
	}

	// If fewer data points than requested resolution, return all
	if len(resources) <= points {
		for _, r := range resources {
			ts := r.CreatedAt.UnixMilli()
			result.Cpu = append(result.Cpu, StatPoint{ts, r.Cpu})
			result.Mem = append(result.Mem, StatPoint{ts, r.Mem})
			result.NetOut = append(result.NetOut, StatPoint{ts, float64(r.NetOut)})
			result.NetIn = append(result.NetIn, StatPoint{ts, float64(r.NetIn)})
			result.DiskW = append(result.DiskW, StatPoint{ts, float64(r.DiskW)})
			result.DiskR = append(result.DiskR, StatPoint{ts, float64(r.DiskR)})
			result.ENCQualityQueue = append(result.ENCQualityQueue, StatPoint{ts, float64(r.ENCQualityQueue)})
			result.ENCAudioQueue = append(result.ENCAudioQueue, StatPoint{ts, float64(r.ENCAudioQueue)})
			result.ENCSubtitleQueue = append(result.ENCSubtitleQueue, StatPoint{ts, float64(r.ENCSubtitleQueue)})
		}
		return result, nil
	}

	// Aggregation logic
	totalDuration := to.Sub(from)
	if totalDuration <= 0 {
		return result, nil
	}
	
	bucketSize := totalDuration / time.Duration(points)
	if bucketSize == 0 {
		bucketSize = time.Second // prevent div by zero
	}

	currentBucketEnd := from.Add(bucketSize)
	
	// Accumulators
	var (
		sumCpu, sumMem, sumNetOut, sumNetIn, sumDiskW, sumDiskR       float64
		sumEncQ, sumEncA, sumEncS                                     float64
		count                                                         float64
	)

	for _, r := range resources {
		// If current record is past the current bucket, close the bucket
		for r.CreatedAt.After(currentBucketEnd) {
			if count > 0 {
				// Finalize bucket
				// Use the middle of the bucket as timestamp? Or end? 
				// ApexCharts usually likes the timestamp. Let's use bucket start or end.
				ts := currentBucketEnd.Add(-bucketSize).UnixMilli()
				
				result.Cpu = append(result.Cpu, StatPoint{ts, sumCpu / count})
				result.Mem = append(result.Mem, StatPoint{ts, sumMem / count})
				result.NetOut = append(result.NetOut, StatPoint{ts, sumNetOut / count})
				result.NetIn = append(result.NetIn, StatPoint{ts, sumNetIn / count})
				result.DiskW = append(result.DiskW, StatPoint{ts, sumDiskW / count})
				result.DiskR = append(result.DiskR, StatPoint{ts, sumDiskR / count})
				result.ENCQualityQueue = append(result.ENCQualityQueue, StatPoint{ts, sumEncQ / count})
				result.ENCAudioQueue = append(result.ENCAudioQueue, StatPoint{ts, sumEncA / count})
				result.ENCSubtitleQueue = append(result.ENCSubtitleQueue, StatPoint{ts, sumEncS / count})
			}
			
			// Reset
			sumCpu, sumMem, sumNetOut, sumNetIn, sumDiskW, sumDiskR = 0, 0, 0, 0, 0, 0
			sumEncQ, sumEncA, sumEncS = 0, 0, 0
			count = 0
			
			// Move to next bucket
			currentBucketEnd = currentBucketEnd.Add(bucketSize)
		}

		// Add to current bucket
		sumCpu += r.Cpu
		sumMem += r.Mem
		sumNetOut += float64(r.NetOut)
		sumNetIn += float64(r.NetIn)
		sumDiskW += float64(r.DiskW)
		sumDiskR += float64(r.DiskR)
		sumEncQ += float64(r.ENCQualityQueue)
		sumEncA += float64(r.ENCAudioQueue)
		sumEncS += float64(r.ENCSubtitleQueue)
		count++
	}

	// Handle last bucket
	if count > 0 {
		ts := currentBucketEnd.Add(-bucketSize).UnixMilli()
		result.Cpu = append(result.Cpu, StatPoint{ts, sumCpu / count})
		result.Mem = append(result.Mem, StatPoint{ts, sumMem / count})
		result.NetOut = append(result.NetOut, StatPoint{ts, sumNetOut / count})
		result.NetIn = append(result.NetIn, StatPoint{ts, sumNetIn / count})
		result.DiskW = append(result.DiskW, StatPoint{ts, sumDiskW / count})
		result.DiskR = append(result.DiskR, StatPoint{ts, sumDiskR / count})
		result.ENCQualityQueue = append(result.ENCQualityQueue, StatPoint{ts, sumEncQ / count})
		result.ENCAudioQueue = append(result.ENCAudioQueue, StatPoint{ts, sumEncA / count})
		result.ENCSubtitleQueue = append(result.ENCSubtitleQueue, StatPoint{ts, sumEncS / count})
	}

	return result, nil
}
