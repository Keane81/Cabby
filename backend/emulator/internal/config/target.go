package config

import (
	"net"
	"net/url"
	"strings"
)

// validateTarget accepts a loopback address always and any other host only with -allow-remote:
// a load tool pointed at the wrong host is hard to take back (FR-016, research.md R-08).
func validateTarget(raw string, allowRemote bool) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return &ValidationError{"target", "must be an http or https URL with a host"}
	}
	if !allowRemote && !isLoopback(u.Hostname()) {
		return &ValidationError{"target", "is not a loopback address; pass -allow-remote to confirm"}
	}
	return nil
}

func isLoopback(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
