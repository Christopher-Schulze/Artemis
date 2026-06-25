package stealth

import (
	"reflect"
	"testing"
)

func TestDefaultCreepJSBypass(t *testing.T) {
	c := DefaultCreepJSBypass()
	if c.ColorScheme != "dark" {
		t.Errorf("ColorScheme = %q, want \"dark\"", c.ColorScheme)
	}
	if c.DeviceScaleFactor != 2 {
		t.Errorf("DeviceScaleFactor = %d, want 2", c.DeviceScaleFactor)
	}
	if c.ScreenWidth != 1920 || c.ScreenHeight != 1080 {
		t.Errorf("Screen = %dx%d, want 1920x1080", c.ScreenWidth, c.ScreenHeight)
	}
	if c.ViewportWidth != 1920 || c.ViewportHeight != 1080 {
		t.Errorf("Viewport = %dx%d, want 1920x1080", c.ViewportWidth, c.ViewportHeight)
	}
	if c.IsMobile {
		t.Errorf("IsMobile = true, want false")
	}
	if c.HasTouch {
		t.Errorf("HasTouch = true, want false")
	}
	if c.ServiceWorkers != "allow" {
		t.Errorf("ServiceWorkers = %q, want \"allow\"", c.ServiceWorkers)
	}
	if !c.IgnoreHTTPSErrors {
		t.Errorf("IgnoreHTTPSErrors = false, want true")
	}
	wantPerms := []string{"geolocation", "notifications"}
	if !reflect.DeepEqual(c.Permissions, wantPerms) {
		t.Errorf("Permissions = %v, want %v", c.Permissions, wantPerms)
	}
}

func TestCreepJSBypassContextOptions(t *testing.T) {
	c := DefaultCreepJSBypass()
	opts := c.ContextOptions()

	if opts["color_scheme"] != "dark" {
		t.Errorf("color_scheme = %v, want \"dark\"", opts["color_scheme"])
	}
	if opts["device_scale_factor"] != 2 {
		t.Errorf("device_scale_factor = %v, want 2", opts["device_scale_factor"])
	}
	screen, ok := opts["screen"].(map[string]int)
	if !ok {
		t.Fatalf("screen is not map[string]int: %T", opts["screen"])
	}
	if screen["width"] != 1920 || screen["height"] != 1080 {
		t.Errorf("screen = %v, want 1920x1080", screen)
	}
	viewport, ok := opts["viewport"].(map[string]int)
	if !ok {
		t.Fatalf("viewport is not map[string]int: %T", opts["viewport"])
	}
	if viewport["width"] != 1920 || viewport["height"] != 1080 {
		t.Errorf("viewport = %v, want 1920x1080", viewport)
	}
	if opts["is_mobile"] != false {
		t.Errorf("is_mobile = %v, want false", opts["is_mobile"])
	}
	if opts["has_touch"] != false {
		t.Errorf("has_touch = %v, want false", opts["has_touch"])
	}
	if opts["service_workers"] != "allow" {
		t.Errorf("service_workers = %v, want \"allow\"", opts["service_workers"])
	}
	if opts["ignore_https_errors"] != true {
		t.Errorf("ignore_https_errors = %v, want true", opts["ignore_https_errors"])
	}
	perms, ok := opts["permissions"].([]string)
	if !ok {
		t.Fatalf("permissions is not []string: %T", opts["permissions"])
	}
	if len(perms) != 2 || perms[0] != "geolocation" || perms[1] != "notifications" {
		t.Errorf("permissions = %v, want [geolocation notifications]", perms)
	}
}

func TestCreepJSBypassApplyToContextMerge(t *testing.T) {
	c := DefaultCreepJSBypass()
	existing := map[string]interface{}{
		"color_scheme": "light",
		"locale":       "en-US",
		"extra":        123,
	}
	merged := c.ApplyToContext(existing)

	// Bypass value should override existing.
	if merged["color_scheme"] != "dark" {
		t.Errorf("color_scheme = %v, want \"dark\" (override)", merged["color_scheme"])
	}
	// Unrelated keys preserved.
	if merged["locale"] != "en-US" {
		t.Errorf("locale = %v, want \"en-US\" (preserved)", merged["locale"])
	}
	if merged["extra"] != 123 {
		t.Errorf("extra = %v, want 123 (preserved)", merged["extra"])
	}
	// Bypass-only keys added.
	if merged["device_scale_factor"] != 2 {
		t.Errorf("device_scale_factor = %v, want 2 (added)", merged["device_scale_factor"])
	}
	if merged["ignore_https_errors"] != true {
		t.Errorf("ignore_https_errors = %v, want true (added)", merged["ignore_https_errors"])
	}
	// Mutated in place.
	if existing["color_scheme"] != "dark" {
		t.Errorf("input map not mutated in place: color_scheme = %v", existing["color_scheme"])
	}
}

func TestCreepJSBypassApplyToContextNil(t *testing.T) {
	c := DefaultCreepJSBypass()
	merged := c.ApplyToContext(nil)
	if merged == nil {
		t.Fatal("ApplyToContext(nil) returned nil")
	}
	if merged["color_scheme"] != "dark" {
		t.Errorf("color_scheme = %v, want \"dark\"", merged["color_scheme"])
	}
}

func TestCreepJSBypassValidateValid(t *testing.T) {
	c := DefaultCreepJSBypass()
	if err := c.Validate(); err != nil {
		t.Errorf("default config Validate() = %v, want nil", err)
	}
}

func TestCreepJSBypassValidateInvalidColorScheme(t *testing.T) {
	c := DefaultCreepJSBypass()
	c.ColorScheme = "purple"
	if err := c.Validate(); err == nil {
		t.Error("Validate() with invalid color_scheme returned nil, want error")
	}
}

func TestCreepJSBypassValidateInvalidScreen(t *testing.T) {
	c := DefaultCreepJSBypass()
	c.ScreenWidth = 0
	if err := c.Validate(); err == nil {
		t.Error("Validate() with zero screen_width returned nil, want error")
	}
	c = DefaultCreepJSBypass()
	c.ScreenHeight = -1
	if err := c.Validate(); err == nil {
		t.Error("Validate() with negative screen_height returned nil, want error")
	}
}

func TestCreepJSBypassValidateInvalidScaleFactor(t *testing.T) {
	c := DefaultCreepJSBypass()
	c.DeviceScaleFactor = 0
	if err := c.Validate(); err == nil {
		t.Error("Validate() with zero device_scale_factor returned nil, want error")
	}
}

func TestCreepJSBypassCustomValues(t *testing.T) {
	c := CreepJSBypass{
		ColorScheme:       "light",
		DeviceScaleFactor: 3,
		ScreenWidth:       2560,
		ScreenHeight:      1440,
		ViewportWidth:     1280,
		ViewportHeight:    720,
		IsMobile:          true,
		HasTouch:          true,
		ServiceWorkers:    "block",
		IgnoreHTTPSErrors: false,
		Permissions:       []string{"camera"},
	}
	if err := c.Validate(); err != nil {
		t.Errorf("custom valid config Validate() = %v, want nil", err)
	}
	opts := c.ContextOptions()
	if opts["color_scheme"] != "light" {
		t.Errorf("color_scheme = %v, want \"light\"", opts["color_scheme"])
	}
	if opts["device_scale_factor"] != 3 {
		t.Errorf("device_scale_factor = %v, want 3", opts["device_scale_factor"])
	}
	screen := opts["screen"].(map[string]int)
	if screen["width"] != 2560 || screen["height"] != 1440 {
		t.Errorf("screen = %v, want 2560x1440", screen)
	}
	viewport := opts["viewport"].(map[string]int)
	if viewport["width"] != 1280 || viewport["height"] != 720 {
		t.Errorf("viewport = %v, want 1280x720", viewport)
	}
	if opts["is_mobile"] != true {
		t.Errorf("is_mobile = %v, want true", opts["is_mobile"])
	}
	if opts["has_touch"] != true {
		t.Errorf("has_touch = %v, want true", opts["has_touch"])
	}
	if opts["service_workers"] != "block" {
		t.Errorf("service_workers = %v, want \"block\"", opts["service_workers"])
	}
	if opts["ignore_https_errors"] != false {
		t.Errorf("ignore_https_errors = %v, want false", opts["ignore_https_errors"])
	}
	perms := opts["permissions"].([]string)
	if len(perms) != 1 || perms[0] != "camera" {
		t.Errorf("permissions = %v, want [camera]", perms)
	}
}
