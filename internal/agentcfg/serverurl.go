package agentcfg

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ErrInsecureServer is a plain http:// server that is not on this machine.
var ErrInsecureServer = errors.New("plain http:// sends the device token unencrypted; use https://, or allow it with --allow-http")

// CheckServerURL accepts https:// URLs, and http:// ones only for a loopback
// host (localhost, 127.0.0.0/8, ::1) unless allowHTTP is set. An empty URL is
// left to the caller ("no server configured").
func CheckServerURL(raw string, allowHTTP bool) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("server URL %q: want https://host[:port]", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if allowHTTP || IsLoopbackHost(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("server URL %q: %w", raw, ErrInsecureServer)
	}
	return fmt.Errorf("server URL %q: want https://host[:port]", raw)
}

// IsLoopbackHost reports whether host names this machine without going over
// a network: "localhost" or a loopback IP literal.
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
