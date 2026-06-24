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
	go w.CleanupResources(ctx)

	for {
		cfg := w.Config()
		v, _ := mem.VirtualMemory()
		c, _ := cpu.Percent(time.Second*2, false)
		n, _ := net.IOCounters(false)
		d, _ := disk.IOCounters(cfg.StatsDriveName)

		printCpu := c[0]
		printRam := v.UsedPercent

		var printNetSent uint64 = 0
		if w.netSent == 0 {
			w.netSent = n[0].BytesSent
		} else {
			printNetSent = n[0].BytesSent - w.netSent
			w.netSent = n[0].BytesSent
		}

		var printNetRecv uint64 = 0
		if w.netRecv == 0 {
			w.netRecv = n[0].BytesRecv
		} else {
			printNetRecv = n[0].BytesRecv - w.netRecv
			w.netRecv = n[0].BytesRecv
		}

		var printDiskWrite uint64 = 0
		if w.diskWrite == 0 {
			w.diskWrite = d[cfg.StatsDriveName].WriteBytes
		} else {
			printDiskWrite = d[cfg.StatsDriveName].WriteBytes - w.diskWrite
			w.diskWrite = d[cfg.StatsDriveName].WriteBytes
		}

		var printDiskRead uint64 = 0
		if w.diskRead == 0 {
			w.diskRead = d[cfg.StatsDriveName].ReadBytes
		} else {
			printDiskRead = d[cfg.StatsDriveName].ReadBytes - w.diskRead
			w.diskRead = d[cfg.StatsDriveName].ReadBytes
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

		if res := w.deps.DB.Create(&models.SystemResource{
			Cpu:              printCpu,
			Mem:              printRam,
			NetOut:           printNetSent / uint64(w.resourcesInterval.Seconds()),
			NetIn:            printNetRecv / uint64(w.resourcesInterval.Seconds()),
			DiskW:            printDiskWrite / uint64(w.resourcesInterval.Seconds()),
			DiskR:            printDiskRead / uint64(w.resourcesInterval.Seconds()),
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
