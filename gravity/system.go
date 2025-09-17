package gravity

import (
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

// getSystemMemory returns the total system memory in bytes on Linux
func getSystemMemory() uint64 {
	if vmStat, err := mem.VirtualMemory(); err == nil {
		return vmStat.Total
	}
	// Fallback if gopsutil fails
	return uint64(0)
}

// getDiskFreeSpace returns the available disk space for the current working directory on Linux
func getDiskFreeSpace(dir string) uint64 {
	if usage, err := disk.Usage(dir); err == nil {
		return usage.Free
	}

	// Fallback if gopsutil fails
	return uint64(0)
}
