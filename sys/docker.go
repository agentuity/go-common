package sys

import (
	"os"
	"regexp"
	"strings"
)

var isCgroupMatch = regexp.MustCompile("(docker|lxc|rkt|libpod|kubepods|containerd)")

// IsRunningInsideContainer returns true if the process is running inside a container environment.
func IsRunningInsideContainer() bool {
	// Docker marks this file
	if Exists("/.dockerenv") {
		return true
	}

	// Podman, Docker, or CRI-O mark this file
	if Exists("/run/.containerenv") {
		return true
	}

	// Fallback to cgroup patterns
	if Exists("/proc/1/cgroup") {
		buf, _ := os.ReadFile("/proc/1/cgroup")
		if len(buf) > 0 {
			contents := strings.TrimSpace(string(buf))
			return isCgroupMatch.MatchString(contents)
		}
	}
	return false
}

// IsRunningInsideDocker returns true if the process is running inside a container environment.
// WARNING: This function is deprecated and will be removed in a future release.
func IsRunningInsideDocker() bool {
	return IsRunningInsideContainer()
}
