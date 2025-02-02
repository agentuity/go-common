package sys

import (
	"net"
	"strings"
)

// GetFreePort asks the kernel for a free open port that is ready to use.
func GetFreePort() (port int, err error) {
	var a *net.TCPAddr
	if a, err = net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close()
			return l.Addr().(*net.TCPAddr).Port, nil
		}
	}
	return
}

// IsLocalhost returns true if the URL is localhost or 127.0.0.1 or 0.0.0.0.
func IsLocalhost(url string) bool {
	// technically 127.0.0.0 – 127.255.255.255 is the loopback range but most people use 127.0.0.1
	return strings.Contains(url, "localhost") || strings.Contains(url, "127.0.0.1") || strings.Contains(url, "0.0.0.0")
}
