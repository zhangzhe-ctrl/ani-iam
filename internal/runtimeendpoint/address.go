package runtimeendpoint

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ValidAddress accepts one explicit TCP endpoint. Transport clients
// still pin the configured TLS identity and registered target independently;
// the address itself establishes no Workload authority.
func ValidAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || strings.ContainsAny(host, "%/\\ \t\r\n") {
		return false
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 || strconv.Itoa(number) != port {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsGlobalUnicast()
	}
	if len(host) > 253 || host != strings.ToLower(host) {
		return false
	}
	// DNS names are deliberately explicit: no resolver schemes, wildcard
	// labels, search URI, zone suffix, empty labels or a trailing root dot.
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func ValidHTTPSOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" {
		return false
	}
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	return ValidAddress(net.JoinHostPort(parsed.Hostname(), port))
}

// ValidListener requires an explicit bind IP; ephemeral ports are limited to loopback test listeners.
func ValidListener(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	n, err := strconv.Atoi(port)
	return err == nil && ip != nil && (ip.IsUnspecified() || ip.IsLoopback() || ip.IsGlobalUnicast()) && !strings.Contains(host, "%") && n >= 0 && n <= 65535 && strconv.Itoa(n) == port && (n > 0 || ip.IsLoopback())
}
