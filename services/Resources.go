package services

import (
	"ch/kirari04/videocms/models"
	"context"
	"log"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

func (w *WorkerGroup) Resources(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		cfg := w.Config()
		v, memErr := mem.VirtualMemory()
		c, cpuErr := cpu.Percent(time.Second*2, false)
		n, netErr := net.IOCounters(false)
		d, diskErr := disk.IOCounters(cfg.StatsDriveName)

		var printCPU, printRAM float64
		if memErr == nil && v != nil {
			printRAM = v.UsedPercent
		}
		if cpuErr == nil && len(c) > 0 {
			printCPU = c[0]
		}
		var printNetSent, printNetRecv uint64
		if netErr == nil && len(n) > 0 {
			printNetSent = counterDelta(n[0].BytesSent, &w.netSent)
			printNetRecv = counterDelta(n[0].BytesRecv, &w.netRecv)
		}
		var printDiskWrite, printDiskRead uint64
		if diskErr == nil {
			if drive, ok := d[cfg.StatsDriveName]; ok {
				printDiskWrite = counterDelta(drive.WriteBytes, &w.diskWrite)
				printDiskRead = counterDelta(drive.ReadBytes, &w.diskRead)
			}
		}

		var printENCQualityQueue int64
		if res := w.deps.DB.Model(&models.Quality{}).
			Where(&models.Quality{
				Ready:  false,
				Failed: false,
			}, "Ready", "Failed").
			Count(&printENCQualityQueue); res.Error != nil {
			log.Println("Failed to count printENCQualityQueue", res.Error)
		}
		var printENCAudioQueue int64
		if res := w.deps.DB.Model(&models.Audio{}).
			Where(&models.Audio{
				Ready:  false,
				Failed: false,
			}, "Ready", "Failed").
			Count(&printENCAudioQueue); res.Error != nil {
			log.Println("Failed to count printENCAudioQueue", res.Error)
		}
		var printENCSubtitleQueue int64
		if res := w.deps.DB.Model(&models.Subtitle{}).
			Where(&models.Subtitle{
				Ready:  false,
				Failed: false,
			}, "Ready", "Failed").
			Count(&printENCSubtitleQueue); res.Error != nil {
			log.Println("Failed to count printENCSubtitleQueue", res.Error)
		}

		intervalSeconds := uint64(w.resourcesInterval.Seconds())
		if intervalSeconds < 1 {
			intervalSeconds = 1
		}
		if res := w.deps.DB.Create(&models.SystemResource{
			Cpu:              printCPU,
			Mem:              printRAM,
			NetOut:           printNetSent / intervalSeconds,
			NetIn:            printNetRecv / intervalSeconds,
			DiskW:            printDiskWrite / intervalSeconds,
			DiskR:            printDiskRead / intervalSeconds,
			ENCQualityQueue:  printENCQualityQueue,
			ENCAudioQueue:    printENCAudioQueue,
			ENCSubtitleQueue: printENCSubtitleQueue,
		}); res.Error != nil {
			log.Println("Failed to save system resources", res.Error)
		}
		if !sleepContext(ctx, w.resourcesInterval) {
			return
		}
	}
}

func counterDelta(current uint64, previous *uint64) uint64 {
	if previous == nil {
		return 0
	}
	before := *previous
	*previous = current
	if before == 0 || current < before {
		return 0
	}
	return current - before
}

func (w *WorkerGroup) CleanupResources(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if !sleepContext(ctx, time.Minute) {
			return
		}
		if res := w.deps.DB.
			Where("created_at < ?", time.Now().Add(time.Hour*24*30*-1)).
			Unscoped().
			Delete(&models.SystemResource{}); res.Error != nil {
			log.Println("Failed to delete system resources")
		}
	}
}
