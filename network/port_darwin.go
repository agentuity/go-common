//go:build darwin

package network

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
)

// DetectListeningTCPPorts scans the system for TCP ports that are currently bound and listening.
// It returns a slice of port numbers, excluding any in the exclude list.
// Works on MacOS by running the lsof command.
func DetectListeningTCPPorts(exclude ...int) ([]int, error) {
	excludeSet := make(map[int]struct{})
	for _, p := range exclude {
		excludeSet[p] = struct{}{}
	}

	cmd := exec.Command("lsof", "-nP", "-iTCP", "-sTCP:LISTEN")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run lsof: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(out))
	portPattern := regexp.MustCompile(`:(\d+)\s+\(LISTEN\)`)

	ports := make(map[int]struct{})

	for scanner.Scan() {
		line := scanner.Text()
		matches := portPattern.FindStringSubmatch(line)
		if len(matches) != 2 {
			continue
		}
		port, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}
		if _, excluded := excludeSet[port]; !excluded {
			ports[port] = struct{}{}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Convert to sorted slice
	var result []int
	for p := range ports {
		result = append(result, p)
	}
	sort.Ints(result)
	return result, nil
}
