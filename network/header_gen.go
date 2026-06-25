package network

// header_gen.go (spec L4366: Browserforge Header Generation).
//
// Scrapling uses browserforge.headers.HeaderGenerator with
// chrome/chromium version 145 to produce realistic, internally
// consistent request headers (User-Agent, Sec-CH-UA, Sec-CH-UA-Platform,
// Accept-Language, Accept-Encoding, Sec-Fetch-*). This module
// reproduces that output for the Go engine without a Python dependency:
// it generates Chrome 145 headers for a given OS with all client hint
// and fetch-metadata headers consistent with the User-Agent string.

import (
	"fmt"
	"net/http"
	"strings"
)

// HeaderGenerator produces realistic Chrome 145 request headers
// for a given browser/OS/device combination (spec L4366).
type HeaderGenerator struct {
	// BrowserName is the browser family. Defaults to "chrome".
	BrowserName string
	// BrowserVersion is the major browser version. Defaults to 145.
	BrowserVersion int
	// OS is the target operating system: "windows", "macos", "linux",
	// or "" to generate for all supported OSes (windows default).
	OS string
	// Device is the device class. Defaults to "desktop".
	Device string
}

// DefaultHeaderGenerator returns a Chrome 145 desktop header
// generator (spec L4366: chromium_version=145, chrome_version=145).
func DefaultHeaderGenerator() HeaderGenerator {
	return HeaderGenerator{
		BrowserName:    "chrome",
		BrowserVersion: 145,
		OS:             "",
		Device:         "desktop",
	}
}

// platformLabel returns the Sec-CH-UA-Platform value for an OS.
func platformLabel(os string) string {
	switch os {
	case "windows":
		return "Windows"
	case "macos":
		return "macOS"
	case "linux":
		return "Linux"
	default:
		return "Windows"
	}
}

// userAgentForOS builds a Chrome 145 User-Agent string for the given
// OS (spec L4366). The strings mirror real Chrome 145 desktop UAs.
func (h HeaderGenerator) userAgentForOS(os string) string {
	ver := h.browserVersionString()
	switch os {
	case "windows":
		return fmt.Sprintf("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", ver)
	case "macos":
		return fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", ver)
	case "linux":
		return fmt.Sprintf("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", ver)
	default:
		return h.userAgentForOS("windows")
	}
}

// browserVersionString returns the full Chrome version token
// (major.0.0.0) for the User-Agent string.
func (h HeaderGenerator) browserVersionString() string {
	v := h.BrowserVersion
	if v <= 0 {
		v = 145
	}
	return fmt.Sprintf("%d.0.0.0", v)
}

// secCHUA builds the Sec-CH-UA client hint header for Chrome 145.
// Format: "Chromium";v="145", "Google Chrome";v="145", "Not?A_Brand";v="24"
func (h HeaderGenerator) secCHUA() string {
	v := h.BrowserVersion
	if v <= 0 {
		v = 145
	}
	return fmt.Sprintf(`"Chromium";v="%d", "Google Chrome";v="%d", "Not?A_Brand";v="24"`, v, v)
}

// resolveOS picks the concrete OS to generate headers for. An empty
// OS means "all"; we default to windows for a deterministic single
// header set (browserforge picks one OS per call).
func (h HeaderGenerator) resolveOS() string {
	switch h.OS {
	case "windows", "macos", "linux":
		return h.OS
	default:
		return "windows"
	}
}

// GenerateForOS generates a consistent Chrome 145 header set for a
// specific OS (spec L4366). The OS must be "windows", "macos", or
// "linux"; any other value falls back to windows.
func (h HeaderGenerator) GenerateForOS(os string) map[string]string {
	platform := platformLabel(os)
	ua := h.userAgentForOS(os)
	headers := map[string]string{
		"User-Agent":                ua,
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
		"Accept-Language":           "en-US,en;q=0.9",
		"Accept-Encoding":           "gzip, deflate, br, zstd",
		"Sec-CH-UA":                 h.secCHUA(),
		"Sec-CH-UA-Mobile":          "?0",
		"Sec-CH-UA-Platform":        fmt.Sprintf(`"%s"`, platform),
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "none",
		"Sec-Fetch-User":            "?1",
		"Upgrade-Insecure-Requests": "1",
	}
	return headers
}

// Generate returns a consistent Chrome 145 header set for the
// generator's configured OS (spec L4366). When OS is empty ("all"),
// windows is used as the deterministic default.
func (h HeaderGenerator) Generate() map[string]string {
	return h.GenerateForOS(h.resolveOS())
}

// AllHeaders returns the generated header set as an http.Header,
// suitable for direct use on an http.Request (spec L4366).
func (h HeaderGenerator) AllHeaders() http.Header {
	hh := make(http.Header, 12)
	for k, v := range h.Generate() {
		hh.Set(k, v)
	}
	return hh
}

// uaContainsOS is a helper used by tests to verify the User-Agent
// string carries the expected OS marker.
func uaContainsOS(ua, os string) bool {
	switch os {
	case "windows":
		return strings.Contains(ua, "Windows NT")
	case "macos":
		return strings.Contains(ua, "Macintosh")
	case "linux":
		return strings.Contains(ua, "Linux x86_64")
	default:
		return false
	}
}
