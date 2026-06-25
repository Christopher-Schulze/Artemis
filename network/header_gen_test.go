package network

import (
	"strings"
	"testing"
)

func TestDefaultHeaderGenerator(t *testing.T) {
	h := DefaultHeaderGenerator()
	if h.BrowserName != "chrome" {
		t.Errorf("BrowserName = %q, want \"chrome\"", h.BrowserName)
	}
	if h.BrowserVersion != 145 {
		t.Errorf("BrowserVersion = %d, want 145", h.BrowserVersion)
	}
	if h.Device != "desktop" {
		t.Errorf("Device = %q, want \"desktop\"", h.Device)
	}
}

func TestHeaderGeneratorDefaultGeneration(t *testing.T) {
	h := DefaultHeaderGenerator()
	headers := h.Generate()
	ua := headers["User-Agent"]
	if !strings.Contains(ua, "Chrome/145.0.0.0") {
		t.Errorf("User-Agent = %q, want Chrome/145.0.0.0", ua)
	}
	if !strings.Contains(ua, "Safari/537.36") {
		t.Errorf("User-Agent missing Safari/537.36: %q", ua)
	}
}

func TestHeaderGeneratorOSWindows(t *testing.T) {
	h := DefaultHeaderGenerator()
	headers := h.GenerateForOS("windows")
	ua := headers["User-Agent"]
	if !strings.Contains(ua, "Windows NT 10.0; Win64; x64") {
		t.Errorf("windows UA = %q, want Windows NT 10.0; Win64; x64", ua)
	}
	if headers["Sec-CH-UA-Platform"] != `"Windows"` {
		t.Errorf("Sec-CH-UA-Platform = %q, want \"Windows\"", headers["Sec-CH-UA-Platform"])
	}
}

func TestHeaderGeneratorOSMacos(t *testing.T) {
	h := DefaultHeaderGenerator()
	headers := h.GenerateForOS("macos")
	ua := headers["User-Agent"]
	if !strings.Contains(ua, "Macintosh; Intel Mac OS X 10_15_7") {
		t.Errorf("macos UA = %q, want Macintosh; Intel Mac OS X 10_15_7", ua)
	}
	if headers["Sec-CH-UA-Platform"] != `"macOS"` {
		t.Errorf("Sec-CH-UA-Platform = %q, want \"macOS\"", headers["Sec-CH-UA-Platform"])
	}
}

func TestHeaderGeneratorOSLinux(t *testing.T) {
	h := DefaultHeaderGenerator()
	headers := h.GenerateForOS("linux")
	ua := headers["User-Agent"]
	if !strings.Contains(ua, "X11; Linux x86_64") {
		t.Errorf("linux UA = %q, want X11; Linux x86_64", ua)
	}
	if headers["Sec-CH-UA-Platform"] != `"Linux"` {
		t.Errorf("Sec-CH-UA-Platform = %q, want \"Linux\"", headers["Sec-CH-UA-Platform"])
	}
}

func TestHeaderGeneratorSecCHUAConsistency(t *testing.T) {
	h := DefaultHeaderGenerator()
	for _, os := range []string{"windows", "macos", "linux"} {
		headers := h.GenerateForOS(os)
		ua := headers["User-Agent"]
		ch := headers["Sec-CH-UA"]
		if !strings.Contains(ch, `"Chromium";v="145"`) {
			t.Errorf("OS %s: Sec-CH-UA = %q, missing Chromium v145", os, ch)
		}
		if !strings.Contains(ch, `"Google Chrome";v="145"`) {
			t.Errorf("OS %s: Sec-CH-UA = %q, missing Google Chrome v145", os, ch)
		}
		if !strings.Contains(ua, "Chrome/145.0.0.0") {
			t.Errorf("OS %s: UA = %q, missing Chrome/145.0.0.0", os, ua)
		}
	}
}

func TestHeaderGeneratorAcceptLanguagePresent(t *testing.T) {
	h := DefaultHeaderGenerator()
	headers := h.Generate()
	if headers["Accept-Language"] != "en-US,en;q=0.9" {
		t.Errorf("Accept-Language = %q, want \"en-US,en;q=0.9\"", headers["Accept-Language"])
	}
}

func TestHeaderGeneratorAllRequiredHeadersPresent(t *testing.T) {
	h := DefaultHeaderGenerator()
	headers := h.Generate()
	required := []string{
		"User-Agent",
		"Accept",
		"Accept-Language",
		"Accept-Encoding",
		"Sec-CH-UA",
		"Sec-CH-UA-Mobile",
		"Sec-CH-UA-Platform",
		"Sec-Fetch-Dest",
		"Sec-Fetch-Mode",
		"Sec-Fetch-Site",
		"Sec-Fetch-User",
		"Upgrade-Insecure-Requests",
	}
	for _, k := range required {
		if v, ok := headers[k]; !ok || v == "" {
			t.Errorf("missing or empty required header %q", k)
		}
	}
	if headers["Sec-CH-UA-Mobile"] != "?0" {
		t.Errorf("Sec-CH-UA-Mobile = %q, want \"?0\"", headers["Sec-CH-UA-Mobile"])
	}
	if headers["Sec-Fetch-Dest"] != "document" {
		t.Errorf("Sec-Fetch-Dest = %q, want \"document\"", headers["Sec-Fetch-Dest"])
	}
	if headers["Sec-Fetch-Mode"] != "navigate" {
		t.Errorf("Sec-Fetch-Mode = %q, want \"navigate\"", headers["Sec-Fetch-Mode"])
	}
	if headers["Sec-Fetch-Site"] != "none" {
		t.Errorf("Sec-Fetch-Site = %q, want \"none\"", headers["Sec-Fetch-Site"])
	}
	if headers["Sec-Fetch-User"] != "?1" {
		t.Errorf("Sec-Fetch-User = %q, want \"?1\"", headers["Sec-Fetch-User"])
	}
	if headers["Upgrade-Insecure-Requests"] != "1" {
		t.Errorf("Upgrade-Insecure-Requests = %q, want \"1\"", headers["Upgrade-Insecure-Requests"])
	}
	if headers["Accept-Encoding"] != "gzip, deflate, br, zstd" {
		t.Errorf("Accept-Encoding = %q, want \"gzip, deflate, br, zstd\"", headers["Accept-Encoding"])
	}
}

func TestHeaderGeneratorCustomVersion(t *testing.T) {
	h := HeaderGenerator{
		BrowserName:    "chrome",
		BrowserVersion: 130,
		OS:             "windows",
		Device:         "desktop",
	}
	headers := h.Generate()
	ua := headers["User-Agent"]
	if !strings.Contains(ua, "Chrome/130.0.0.0") {
		t.Errorf("custom version UA = %q, want Chrome/130.0.0.0", ua)
	}
	ch := headers["Sec-CH-UA"]
	if !strings.Contains(ch, `"Chromium";v="130"`) {
		t.Errorf("custom version Sec-CH-UA = %q, missing v130", ch)
	}
}

func TestHeaderGeneratorAllHeaders(t *testing.T) {
	h := DefaultHeaderGenerator()
	hh := h.AllHeaders()
	if hh == nil {
		t.Fatal("AllHeaders() returned nil")
	}
	ua := hh.Get("User-Agent")
	if !strings.Contains(ua, "Chrome/145.0.0.0") {
		t.Errorf("AllHeaders User-Agent = %q, want Chrome/145.0.0.0", ua)
	}
	if hh.Get("Accept-Language") != "en-US,en;q=0.9" {
		t.Errorf("AllHeaders Accept-Language = %q, want en-US,en;q=0.9", hh.Get("Accept-Language"))
	}
}

func TestHeaderGeneratorUnknownOSFallsBackToWindows(t *testing.T) {
	h := DefaultHeaderGenerator()
	headers := h.GenerateForOS("solaris")
	ua := headers["User-Agent"]
	if !strings.Contains(ua, "Windows NT") {
		t.Errorf("unknown OS UA = %q, want windows fallback", ua)
	}
}
