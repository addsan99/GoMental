package httpapi

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// validateImportURLForSSRF is an adapter-level guard applied before the core
// ImportURL fetch runs in server mode. Because the server performs the fetch on
// behalf of a network caller, an attacker could otherwise coax it into reaching
// internal services (SSRF). We resolve the host and reject any result that maps
// to a loopback, private, link-local, or otherwise non-global-unicast address.
//
// Desktop mode never calls this (the desktop user is trusted), so the existing
// single-user import behavior is unchanged (Guardrail G1/G5).
func validateImportURLForSSRF(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("missing host")
	}
	// If the host is a literal IP, check it directly.
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return fmt.Errorf("host resolves to a non-public address: %s", ip)
		}
		return nil
	}
	// Otherwise resolve and reject if ANY resolved address is non-public.
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("could not resolve host %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("host %q did not resolve", host)
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return fmt.Errorf("host %q resolves to a non-public address: %s", host, ip)
		}
	}
	return nil
}

// isPublicIP reports whether ip is a globally routable unicast address.
func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	// Reject IPv4 broadcast and carrier-grade NAT / benchmarking ranges that
	// net.IP helpers do not classify as private.
	if v4 := ip.To4(); v4 != nil {
		// 100.64.0.0/10 (CGNAT), 192.0.0.0/24, 198.18.0.0/15 (benchmark)
		switch {
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127:
			return false
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 0:
			return false
		case v4[0] == 198 && (v4[1] == 18 || v4[1] == 19):
			return false
		}
	}
	return true
}
