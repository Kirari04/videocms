package services

import (
	"fmt"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

var resourcesInterval = time.Second * 1
var netSent uint64 = 0
var netRecv uint64 = 0

var diskWrite uint64 = 0
var diskRead uint64 = 0

func Resources() {
	time.Sleep(resourcesInterval)
	fmt.Printf("MEM\t\tCPU\t\tNET-OUT\t\tNET-IN\t\tDISK-W\t\tDISK-R\n")
	for {
		v, _ := mem.VirtualMemory()
		c, _ := cpu.Percent(time.Second*2, false)
		n, _ := net.IOCounters(false)
		d, _ := disk.IOCounters("nvme0n1")

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
			diskWrite = d["nvme0n1"].WriteBytes
		} else {
			printDiskWrite = d["nvme0n1"].WriteBytes - diskWrite
			diskWrite = d["nvme0n1"].WriteBytes
		}

		var printDiskRead uint64 = 0
		if diskRead == 0 {
			diskRead = d["nvme0n1"].ReadBytes
		} else {
			printDiskRead = d["nvme0n1"].ReadBytes - diskRead
			diskRead = d["nvme0n1"].ReadBytes
		}
		fmt.Printf(
			"%f%%\t%f%%\t%s/s   \t%s/s   \t%v/s   \t%v/s\n",
			printRam,
			printCpu,
			humanize.Bytes(printNetSent/uint64(resourcesInterval.Seconds())),
			humanize.Bytes(printNetRecv/uint64(resourcesInterval.Seconds())),
			humanize.Bytes(printDiskWrite/uint64(resourcesInterval.Seconds())),
			humanize.Bytes(printDiskRead/uint64(resourcesInterval.Seconds())),
		)
		time.Sleep(resourcesInterval)
	}
}
