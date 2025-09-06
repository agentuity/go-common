package sys

import (
	"net"
	neturl "net/url"
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

// IsLocalhost returns true if the input points to localhost/loopback or an unspecified address.
func IsLocalhost(url string) bool {
	// Accept either a full URL or a bare host[:port].
	host := url
	if u, err := neturl.Parse(url); err == nil && u.Host != "" {
		host = u.Hostname() // strips [] for IPv6
	} else if h, _, err := net.SplitHostPort(url); err == nil {
		host = h
	} else {
		host = strings.Trim(host, "[]")
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsUnspecified() // 127/8, ::1, 0.0.0.0, ::
	}
	return false
}
