package services

import (
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

var resourcesInterval = time.Second * 10
var netSent uint64 = 0
var netRecv uint64 = 0

var diskWrite uint64 = 0
var diskRead uint64 = 0

func Resources() {
	go func() {
		// delete stats older than 30 days
		for {
			time.Sleep(time.Minute * 1)
			if res := inits.DB.
				Where("created_at < ?", time.Now().Add(time.Hour*24*30*-1)).
				Unscoped().
				Delete(&models.SystemResource{}); res.Error != nil {
				log.Println("Failed to delete system resources")
			}
		}
	}()
	for {
		v, _ := mem.VirtualMemory()
		c, _ := cpu.Percent(time.Second*2, false)
		n, _ := net.IOCounters(false)
		d, _ := disk.IOCounters(config.ENV.StatsDriveName)

		printCpu := c[0]
		printRam := v.UsedPercent

		var printNetSent uint64 = 0
		if netSent == 0 {
			netSent = n[0].BytesSent
		} else {
			printNetSent = n[0].BytesSent - netSent
			netSent = n[0].BytesSent
		}

		var printNetRecv uint64 = 0
		if netRecv == 0 {
			netRecv = n[0].BytesRecv
		} else {
			printNetRecv = n[0].BytesRecv - netRecv
			netRecv = n[0].BytesRecv
		}

		var printDiskWrite uint64 = 0
		if diskWrite == 0 {
			diskWrite = d[config.ENV.StatsDriveName].WriteBytes
		} else {
			printDiskWrite = d[config.ENV.StatsDriveName].WriteBytes - diskWrite
			diskWrite = d[config.ENV.StatsDriveName].WriteBytes
		}

		var printDiskRead uint64 = 0
		if diskRead == 0 {
			diskRead = d[config.ENV.StatsDriveName].ReadBytes
		} else {
			printDiskRead = d[config.ENV.StatsDriveName].ReadBytes - diskRead
			diskRead = d[config.ENV.StatsDriveName].ReadBytes
		}

		var printENCQualityQueue int64
		if res := inits.DB.Model(&models.Quality{}).
			Where(&models.Quality{
				Ready:  false,
				Failed: false,
			}, "Ready", "Failed").
			Count(&printENCQualityQueue); res.Error != nil {
			log.Println("Failed to count printENCQualityQueue", res.Error)
		}
		var printENCAudioQueue int64
		if res := inits.DB.Model(&models.Audio{}).
			Where(&models.Audio{
				Ready:  false,
				Failed: false,
			}, "Ready", "Failed").
			Count(&printENCAudioQueue); res.Error != nil {
			log.Println("Failed to count printENCAudioQueue", res.Error)
		}
		var printENCSubtitleQueue int64
		if res := inits.DB.Model(&models.Subtitle{}).
			Where(&models.Subtitle{
				Ready:  false,
				Failed: false,
			}, "Ready", "Failed").
			Count(&printENCSubtitleQueue); res.Error != nil {
			log.Println("Failed to count printENCSubtitleQueue", res.Error)
		}

		if res := inits.DB.Create(&models.SystemResource{
			Cpu:              printCpu,
			Mem:              printRam,
			NetOut:           printNetSent / uint64(resourcesInterval.Seconds()),
			NetIn:            printNetRecv / uint64(resourcesInterval.Seconds()),
			DiskW:            printDiskWrite / uint64(resourcesInterval.Seconds()),
			DiskR:            printDiskRead / uint64(resourcesInterval.Seconds()),
			ENCQualityQueue:  printENCQualityQueue,
			ENCAudioQueue:    printENCAudioQueue,
			ENCSubtitleQueue: printENCSubtitleQueue,
		}); res.Error != nil {
			log.Println("Failed to save system resources", res.Error)
		}
		time.Sleep(resourcesInterval)
	}
}
