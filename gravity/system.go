package gravity

import (
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// getSystemMemory returns the total system memory in bytes on Linux
func getSystemMemory() uint64 {
	if vmStat, err := mem.VirtualMemory(); err == nil {
		return vmStat.Total
	}
	// Fallback if gopsutil fails
	return uint64(0)
}

// getDiskSpace returns the available disk space for the current working directory on Linux
func getDiskSpace(dir string) uint64 {
	if usage, err := disk.Usage(dir); err == nil {
		return usage.Total
	}

	// Fallback if gopsutil fails
	return uint64(0)
}

// getCPUUsage returns the current CPU usage percentage
func getCPUUsage() float64 {
	if percentages, err := cpu.Percent(time.Second, false); err == nil && len(percentages) > 0 {
		return percentages[0]
	}
	// Return 0 if unable to get CPU usage
	return 0.0
}
