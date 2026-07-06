package stealth

// creepjs_bypass.go (spec L4364: CreepJS Dark Mode Bypass).
//
// CreepJS flags sessions whose prefers-color-scheme media query
// resolves to light while the OS reports dark mode (and vice versa).
// Scrapling resolves this by forcing color_scheme "dark" together
// with a device_scale_factor of 2 and a 1920x1080 screen/viewport,
// matching the StealthySessionMixin defaults in
// scrapling/engines/_browsers/_base.py.
//
// This module defines the CreepJSBypass configuration consumed by
// the browser launch layer to set color scheme, device metrics,
// viewport, touch, service workers, HTTPS errors, and permissions
// consistently with a real Chromium dark-mode session.

import (
	"errors"
	"fmt"
)

// CreepJSBypass configures a browser context to bypass CreepJS
// prefersLightColor and related device-metric checks (spec L4364).
type CreepJSBypass struct {
	// ColorScheme forces the prefers-color-scheme media query.
	// "dark" bypasses the prefersLightColor check in CreepJS.
	ColorScheme string `json:"color_scheme"`
	// DeviceScaleFactor is the CSS device pixel ratio. Chrome on
	// HiDPI displays reports 2; matching this avoids the
	// devicePixelRatio anomaly check.
	DeviceScaleFactor int `json:"device_scale_factor"`
	// ScreenWidth is the reported screen.width in CSS pixels.
	ScreenWidth int `json:"screen_width"`
	// ScreenHeight is the reported screen.height in CSS pixels.
	ScreenHeight int `json:"screen_height"`
	// ViewportWidth is the window.innerWidth / layout viewport width.
	ViewportWidth int `json:"viewport_width"`
	// ViewportHeight is the window.innerHeight / layout viewport height.
	ViewportHeight int `json:"viewport_height"`
	// IsMobile reports the mobile user-agent metadata flag.
	IsMobile bool `json:"is_mobile"`
	// HasTouch reports whether touch input is available.
	HasTouch bool `json:"has_touch"`
	// ServiceWorkers controls the "serviceWorkers" context option:
	// "allow" lets pages register service workers (Chrome default).
	ServiceWorkers string `json:"service_workers"`
	// IgnoreHTTPSErrors allows self-signed / invalid TLS certs,
	// matching StealthySessionMixin.ignore_https_errors.
	IgnoreHTTPSErrors bool `json:"ignore_https_errors"`
	// Permissions is the list of permission names to auto-grant
	// (geolocation, notifications) so CreepJS permission probes
	// resolve like a real user session.
	Permissions []string `json:"permissions"`
}

// DefaultCreepJSBypass returns the CreepJS-bypassing configuration
// matching Scrapling's StealthySessionMixin defaults (spec L4364):
// dark color scheme, device scale factor 2, 1920x1080 screen and
// viewport, desktop (no touch), service workers allowed, HTTPS
// errors ignored, geolocation + notifications granted.
func DefaultCreepJSBypass() CreepJSBypass {
	return CreepJSBypass{
		ColorScheme:       "dark",
		DeviceScaleFactor: 2,
		ScreenWidth:       1920,
		ScreenHeight:      1080,
		ViewportWidth:     1920,
		ViewportHeight:    1080,
		IsMobile:          false,
		HasTouch:          false,
		ServiceWorkers:    "allow",
		IgnoreHTTPSErrors: true,
		Permissions:       []string{"geolocation", "notifications"},
	}
}

// ContextOptions returns the bypass configuration as a
// map[string]interface{} suitable for passing to a Playwright/CDP
// browser context launcher. All fields are serialized under their
// canonical context-option keys.
func (c CreepJSBypass) ContextOptions() map[string]interface{} {
	opts := map[string]interface{}{
		"color_scheme":        c.ColorScheme,
		"device_scale_factor": c.DeviceScaleFactor,
		"screen": map[string]int{
			"width":  c.ScreenWidth,
			"height": c.ScreenHeight,
		},
		"viewport": map[string]int{
			"width":  c.ViewportWidth,
			"height": c.ViewportHeight,
		},
		"is_mobile":           c.IsMobile,
		"has_touch":           c.HasTouch,
		"service_workers":     c.ServiceWorkers,
		"ignore_https_errors": c.IgnoreHTTPSErrors,
		"permissions":         append([]string(nil), c.Permissions...),
	}
	return opts
}

// ApplyToContext merges the bypass options into an existing context
// map. Existing keys are overwritten by the bypass values so the
// CreepJS-bypass settings always take precedence. The input map is
// mutated in place and also returned for chaining.
func (c CreepJSBypass) ApplyToContext(options map[string]interface{}) map[string]interface{} {
	if options == nil {
		options = make(map[string]interface{})
	}
	for k, v := range c.ContextOptions() {
		options[k] = v
	}
	return options
}

// Validate checks the bypass configuration for internal consistency
// (spec L4364): ColorScheme must be "dark" or "light", screen
// dimensions and device scale factor must be positive.
func (c CreepJSBypass) Validate() error {
	if c.ColorScheme != "dark" && c.ColorScheme != "light" {
		return fmt.Errorf("creepjs bypass: color_scheme must be \"dark\" or \"light\", got %q", c.ColorScheme)
	}
	if c.ScreenWidth <= 0 {
		return errors.New("creepjs bypass: screen_width must be positive")
	}
	if c.ScreenHeight <= 0 {
		return errors.New("creepjs bypass: screen_height must be positive")
	}
	if c.DeviceScaleFactor <= 0 {
		return errors.New("creepjs bypass: device_scale_factor must be positive")
	}
	return nil
}
