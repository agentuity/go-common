//go:build linux

package network

import (
	"bufio"
	"os"
	"sort"
	"strconv"
	"strings"
)

// DetectListeningTCPPorts scans the system for TCP ports that are currently bound and listening.
// It returns a slice of port numbers, excluding any in the exclude list.
// Works on Linux by reading /proc/net/tcp and /proc/net/tcp6.
func DetectListeningTCPPorts(exclude ...int) ([]int, error) {
	excludeSet := make(map[int]struct{})
	for _, p := range exclude {
		excludeSet[p] = struct{}{}
	}

	files := []string{"/proc/net/tcp", "/proc/net/tcp6"}
	var ports []int

	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			continue // skip if file doesn't exist or can't be read
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		// Skip the header line
		if scanner.Scan() {
			for scanner.Scan() {
				fields := strings.Fields(scanner.Text())
				if len(fields) < 4 {
					continue
				}

				localAddr := fields[1]
				state := fields[3]
				if state != "0A" { // 0A means LISTEN
					continue
				}

				// localAddr example: 0100007F:1F90
				parts := strings.Split(localAddr, ":")
				if len(parts) != 2 {
					continue
				}

				portHex := parts[1]
				portDec, err := strconv.ParseInt(portHex, 16, 32)
				if err != nil {
					continue
				}
				port := int(portDec)

				if _, excluded := excludeSet[port]; !excluded {
					ports = append(ports, port)
				}
			}
		}
	}

	sort.Ints(ports)
	return ports, nil
}
