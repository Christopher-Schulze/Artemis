package profile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ==================== cache.go tests ====================

// TestTASK2239_CacheControlCreation verifies NewBrowserCacheControl
// creates a control with default settings (spec L4028).
func TestTASK2239_CacheControlCreation(t *testing.T) {
	c := NewBrowserCacheControl("test-profile")
	if c.ProfileName != "test-profile" {
		t.Errorf("profile name: got %s, want test-profile", c.ProfileName)
	}
	if !c.Enabled {
		t.Error("cache should be enabled by default")
	}
	if c.Policy != CachePolicyMaxAge {
		t.Errorf("policy: got %s, want max_age", c.Policy)
	}
	if c.MaxAgeSeconds != 3600 {
		t.Errorf("max age: got %d, want 3600", c.MaxAgeSeconds)
	}
}

// TestTASK2239_CacheRecordHitMiss verifies hit/miss recording
// (spec L4028: cache metrics).
func TestTASK2239_CacheRecordHitMiss(t *testing.T) {
	c := NewBrowserCacheControl("p")
	c.RecordHit(100)
	c.RecordHit(200)
	c.RecordMiss()
	stats := c.Stats()
	if stats.HitCount != 2 {
		t.Errorf("hits: got %d, want 2", stats.HitCount)
	}
	if stats.MissCount != 1 {
		t.Errorf("misses: got %d, want 1", stats.MissCount)
	}
	if stats.CurrentBytes != 300 {
		t.Errorf("bytes: got %d, want 300", stats.CurrentBytes)
	}
}

// TestTASK2239_CacheHitRate verifies hit rate calculation
// (spec L4028: cache metrics).
func TestTASK2239_CacheHitRate(t *testing.T) {
	c := NewBrowserCacheControl("p")
	c.RecordHit(100)
	c.RecordHit(200)
	c.RecordMiss()
	c.RecordMiss()
	rate := c.HitRate()
	expected := 2.0 / 4.0
	if rate != expected {
		t.Errorf("hit rate: got %v, want %v", rate, expected)
	}
}

// TestTASK2239_CacheHitRateEmpty verifies hit rate is 0 for empty cache.
func TestTASK2239_CacheHitRateEmpty(t *testing.T) {
	c := NewBrowserCacheControl("p")
	if c.HitRate() != 0 {
		t.Error("empty cache hit rate should be 0")
	}
}

// TestTASK2239_CacheClear verifies Clear resets the cache
// (spec L4028: clear-cache).
func TestTASK2239_CacheClear(t *testing.T) {
	c := NewBrowserCacheControl("p")
	c.RecordHit(500)
	c.Clear()
	stats := c.Stats()
	if stats.CurrentBytes != 0 {
		t.Errorf("after clear: got %d bytes, want 0", stats.CurrentBytes)
	}
	if stats.ClearCount != 1 {
		t.Errorf("clear count: got %d, want 1", stats.ClearCount)
	}
	if stats.LastClearedAt.IsZero() {
		t.Error("last cleared at should be set")
	}
}

// TestTASK2239_CacheEviction verifies eviction recording
// (spec L4028: cache metrics).
func TestTASK2239_CacheEviction(t *testing.T) {
	c := NewBrowserCacheControl("p")
	c.RecordHit(500)
	c.RecordEviction(200)
	stats := c.Stats()
	if stats.EvictionCount != 1 {
		t.Errorf("evictions: got %d, want 1", stats.EvictionCount)
	}
	if stats.CurrentBytes != 300 {
		t.Errorf("bytes after eviction: got %d, want 300", stats.CurrentBytes)
	}
}

// TestTASK2239_CacheEvictionNotNegative verifies bytes don't go negative.
func TestTASK2239_CacheEvictionNotNegative(t *testing.T) {
	c := NewBrowserCacheControl("p")
	c.RecordHit(100)
	c.RecordEviction(500) // evict more than we have
	stats := c.Stats()
	if stats.CurrentBytes < 0 {
		t.Errorf("bytes should not be negative: got %d", stats.CurrentBytes)
	}
	if stats.CurrentBytes != 0 {
		t.Errorf("bytes should be clamped to 0: got %d", stats.CurrentBytes)
	}
}

// TestTASK2239_CacheIsExpiredMaxAge verifies max-age expiration
// (spec L4028: max-age enforcement).
func TestTASK2239_CacheIsExpiredMaxAge(t *testing.T) {
	c := NewBrowserCacheControl("p")
	c.SetPolicy(CachePolicyMaxAge, 1) // 1 second max age
	if c.IsExpired(time.Now()) {
		t.Error("fresh resource should not be expired")
	}
	old := time.Now().Add(-2 * time.Second)
	if !c.IsExpired(old) {
		t.Error("2-second-old resource should be expired with 1s max-age")
	}
}

// TestTASK2239_CacheIsExpiredDisabled verifies disabled cache expires
// everything (spec L4028).
func TestTASK2239_CacheIsExpiredDisabled(t *testing.T) {
	c := NewBrowserCacheControl("p")
	c.SetPolicy(CachePolicyDisabled, 0)
	if !c.IsExpired(time.Now()) {
		t.Error("disabled cache should expire everything")
	}
}

// TestTASK2239_CacheIsExpiredDefault verifies default policy never
// expires (spec L4028).
func TestTASK2239_CacheIsExpiredDefault(t *testing.T) {
	c := NewBrowserCacheControl("p")
	c.SetPolicy(CachePolicyDefault, 0)
	if c.IsExpired(time.Now().Add(-100 * time.Hour)) {
		t.Error("default policy should never expire")
	}
}

// TestTASK2239_CacheShouldEvict verifies max-size eviction logic
// (spec L4028: cache size management).
func TestTASK2239_CacheShouldEvict(t *testing.T) {
	c := NewBrowserCacheControl("p")
	c.SetMaxSize(1000)
	c.RecordHit(800)
	if c.ShouldEvict(100) {
		t.Error("100 bytes should fit in 1000 max with 800 used")
	}
	if !c.ShouldEvict(300) {
		t.Error("300 bytes should NOT fit in 1000 max with 800 used")
	}
}

// TestTASK2239_CacheShouldEvictNoMaxSize verifies no max size means
// no eviction.
func TestTASK2239_CacheShouldEvictNoMaxSize(t *testing.T) {
	c := NewBrowserCacheControl("p")
	c.RecordHit(999999999)
	if c.ShouldEvict(999999999) {
		t.Error("no max size should never evict")
	}
}

// TestTASK2239_CacheSetEnabled verifies enabling/disabling cache.
func TestTASK2239_CacheSetEnabled(t *testing.T) {
	c := NewBrowserCacheControl("p")
	c.SetEnabled(false)
	if c.Enabled {
		t.Error("cache should be disabled")
	}
	if !c.IsExpired(time.Now()) {
		t.Error("disabled cache should expire everything")
	}
}

// TestTASK2239_CacheNilSafe verifies nil cache control is safe.
func TestTASK2239_CacheNilSafe(t *testing.T) {
	var c *BrowserCacheControl
	c.RecordHit(100)
	c.RecordMiss()
	c.RecordEviction(100)
	c.Clear()
	c.SetPolicy(CachePolicyDefault, 0)
	c.SetEnabled(false)
	c.SetMaxSize(100)
	_ = c.Stats()
	_ = c.HitRate()
	_ = c.ShouldEvict(100)
	_ = c.IsExpired(time.Now())
	_ = c.String()
}

// TestTASK2239_CacheString verifies the String method.
func TestTASK2239_CacheString(t *testing.T) {
	c := NewBrowserCacheControl("p")
	s := c.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// TestTASK2239_CacheControlManagerGet verifies the manager creates
// and returns per-profile controls (spec L4028: per-profile isolation).
func TestTASK2239_CacheControlManagerGet(t *testing.T) {
	m := NewCacheControlManager()
	c1 := m.Get("profile1")
	c2 := m.Get("profile2")
	c1b := m.Get("profile1")
	if c1 != c1b {
		t.Error("same profile should return same control instance")
	}
	if c1 == c2 {
		t.Error("different profiles should return different instances")
	}
}

// TestTASK2239_CacheControlManagerClearAll verifies ClearAll clears
// all profiles.
func TestTASK2239_CacheControlManagerClearAll(t *testing.T) {
	m := NewCacheControlManager()
	c1 := m.Get("p1")
	c2 := m.Get("p2")
	c1.RecordHit(100)
	c2.RecordHit(200)
	m.ClearAll()
	if c1.Stats().CurrentBytes != 0 {
		t.Error("p1 should be cleared")
	}
	if c2.Stats().CurrentBytes != 0 {
		t.Error("p2 should be cleared")
	}
}

// TestTASK2239_CacheControlManagerAllStats verifies AllStats returns
// stats for all profiles.
func TestTASK2239_CacheControlManagerAllStats(t *testing.T) {
	m := NewCacheControlManager()
	m.Get("p1").RecordHit(100)
	m.Get("p2").RecordHit(200)
	all := m.AllStats()
	if len(all) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(all))
	}
	if all["p1"].CurrentBytes != 100 {
		t.Errorf("p1 bytes: got %d, want 100", all["p1"].CurrentBytes)
	}
	if all["p2"].CurrentBytes != 200 {
		t.Errorf("p2 bytes: got %d, want 200", all["p2"].CurrentBytes)
	}
}

// TestTASK2239_CacheControlManagerNilSafe verifies nil manager is safe.
func TestTASK2239_CacheControlManagerNilSafe(t *testing.T) {
	var m *CacheControlManager
	if m.Get("p") != nil {
		t.Error("nil manager Get should return nil")
	}
	m.ClearAll()
	if m.AllStats() != nil {
		t.Error("nil manager AllStats should return nil")
	}
}

// ==================== proxy_profiles.go tests ====================

// TestTASK2239_GeoModeConstants verifies the geo mode constants
// (spec L4028).
func TestTASK2239_GeoModeConstants(t *testing.T) {
	if GeoModeExplicitWins != "explicit_wins" {
		t.Error("explicit_wins constant mismatch")
	}
	if GeoModeProxyLocked != "proxy_locked" {
		t.Error("proxy_locked constant mismatch")
	}
}

// TestTASK2239_ProxyProfileStoreLoad verifies loading proxy profiles
// from JSON (spec L4028).
func TestTASK2239_ProxyProfileStoreLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy-profiles.json")
	jsonContent := `{
		"de-proxy": {
			"server": "http://proxy.de:8080",
			"username": "user1",
			"password": "pass1",
			"locale": "de-DE",
			"timezoneId": "Europe/Berlin",
			"geolocation": {"latitude": 52.52, "longitude": 13.405}
		},
		"us-proxy": {
			"server": "http://proxy.us:8080"
		}
	}`
	if err := os.WriteFile(path, []byte(jsonContent), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := LoadProxyProfiles(path)
	if err != nil {
		t.Fatalf("LoadProxyProfiles: %v", err)
	}
	p, ok := store.Get("de-proxy")
	if !ok {
		t.Fatal("de-proxy should be found")
	}
	if p.Server != "http://proxy.de:8080" {
		t.Errorf("server: got %s", p.Server)
	}
	if p.Locale != "de-DE" {
		t.Errorf("locale: got %s", p.Locale)
	}
	if p.Geolocation == nil || p.Geolocation.Latitude != 52.52 {
		t.Error("geolocation mismatch")
	}
}

// TestTASK2239_ProxyProfileStoreCaseInsensitive verifies case-insensitive
// name lookup (spec L4028).
func TestTASK2239_ProxyProfileStoreCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy-profiles.json")
	jsonContent := `{"MyProxy": {"server": "http://proxy:8080"}}`
	if err := os.WriteFile(path, []byte(jsonContent), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := LoadProxyProfiles(path)
	if err != nil {
		t.Fatalf("LoadProxyProfiles: %v", err)
	}
	if _, ok := store.Get("myproxy"); !ok {
		t.Error("lowercase lookup should work")
	}
	if _, ok := store.Get("MYPROXY"); !ok {
		t.Error("uppercase lookup should work")
	}
}

// TestTASK2239_ProxyProfileStoreEmptyPath verifies empty path returns
// empty store (spec L4028).
func TestTASK2239_ProxyProfileStoreEmptyPath(t *testing.T) {
	store, err := LoadProxyProfiles("")
	if err != nil {
		t.Fatalf("empty path should not error: %v", err)
	}
	if len(store.Names()) != 0 {
		t.Error("empty path should produce empty store")
	}
}

// TestTASK2239_ProxyProfileStoreValidationLat verifies latitude
// validation (spec L4028: lat∈[-90,90]).
func TestTASK2239_ProxyProfileStoreValidationLat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy-profiles.json")
	jsonContent := `{"bad": {"server": "http://p:80", "geolocation": {"latitude": 91, "longitude": 0}}}`
	if err := os.WriteFile(path, []byte(jsonContent), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProxyProfiles(path)
	if err == nil {
		t.Fatal("latitude > 90 should fail validation")
	}
}

// TestTASK2239_ProxyProfileStoreValidationLong verifies longitude
// validation (spec L4028: long∈[-180,180]).
func TestTASK2239_ProxyProfileStoreValidationLong(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy-profiles.json")
	jsonContent := `{"bad": {"server": "http://p:80", "geolocation": {"latitude": 0, "longitude": 181}}}`
	if err := os.WriteFile(path, []byte(jsonContent), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProxyProfiles(path)
	if err == nil {
		t.Fatal("longitude > 180 should fail validation")
	}
}

// TestTASK2239_ProxyProfileStoreValidationLocale verifies locale
// validation (spec L4028: BCP47 regex).
func TestTASK2239_ProxyProfileStoreValidationLocale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy-profiles.json")
	jsonContent := `{"bad": {"server": "http://p:80", "locale": "invalid_locale_1234567890123456789012345678901234567890"}}`
	if err := os.WriteFile(path, []byte(jsonContent), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProxyProfiles(path)
	if err == nil {
		t.Fatal("invalid locale should fail validation")
	}
}

// TestTASK2239_ProxyProfileStoreValidationEmptyServer verifies empty
// server is rejected (spec L4028).
func TestTASK2239_ProxyProfileStoreValidationEmptyServer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy-profiles.json")
	jsonContent := `{"bad": {"server": ""}}`
	if err := os.WriteFile(path, []byte(jsonContent), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProxyProfiles(path)
	if err == nil {
		t.Fatal("empty server should fail validation")
	}
}

// TestTASK2239_GetConfiguredServerProxy verifies the fallback server
// proxy resolution (spec L4028, research getConfiguredServerProxy).
func TestTASK2239_GetConfiguredServerProxy(t *testing.T) {
	proxy := GetConfiguredServerProxy(ConfiguredServerProxy{
		Host: "proxy.example.com",
		Port: 8080,
	})
	if proxy == nil {
		t.Fatal("expected non-nil proxy")
	}
	if proxy.Server != "http://proxy.example.com:8080" {
		t.Errorf("server: got %s", proxy.Server)
	}
	if proxy.Source != ProxySourceServerDefault {
		t.Errorf("source: got %s, want server-default", proxy.Source)
	}
}

// TestTASK2239_GetConfiguredServerProxyEmpty verifies empty config
// returns nil.
func TestTASK2239_GetConfiguredServerProxyEmpty(t *testing.T) {
	if GetConfiguredServerProxy(ConfiguredServerProxy{}) != nil {
		t.Error("empty config should return nil")
	}
}

// TestTASK2239_NormalizeRawProxy verifies raw proxy normalization
// (spec L4028, research normalizeRawProxy).
func TestTASK2239_NormalizeRawProxy(t *testing.T) {
	proxy, err := NormalizeRawProxy(RawProxyOverride{
		Host: "proxy.example.com",
		Port: 8080,
	})
	if err != nil {
		t.Fatalf("NormalizeRawProxy: %v", err)
	}
	if proxy.Server != "http://proxy.example.com:8080" {
		t.Errorf("server: got %s", proxy.Server)
	}
	if proxy.Source != ProxySourceRawCredentials {
		t.Errorf("source: got %s, want raw-credentials", proxy.Source)
	}
}

// TestTASK2239_NormalizeRawProxyEmpty verifies empty raw proxy errors.
func TestTASK2239_NormalizeRawProxyEmpty(t *testing.T) {
	_, err := NormalizeRawProxy(RawProxyOverride{})
	if err == nil {
		t.Fatal("empty raw proxy should error")
	}
}

// TestTASK2239_ResolveSessionProxyNamedProfile verifies named profile
// resolution (spec L4028).
func TestTASK2239_ResolveSessionProxyNamedProfile(t *testing.T) {
	store := NewProxyProfileStore()
	store.profiles["de-proxy"] = ProxyProfileConfig{
		Server: "http://proxy.de:8080",
		Locale: "de-DE",
	}
	proxy, err := ResolveSessionProxy(SessionProfileInput{
		ProxyProfile: "de-proxy",
	}, store, nil)
	if err != nil {
		t.Fatalf("ResolveSessionProxy: %v", err)
	}
	if proxy.Source != ProxySourceNamedProfile {
		t.Errorf("source: got %s, want named-profile", proxy.Source)
	}
	if proxy.Server != "http://proxy.de:8080" {
		t.Errorf("server: got %s", proxy.Server)
	}
	if proxy.Locale != "de-DE" {
		t.Errorf("locale: got %s", proxy.Locale)
	}
}

// TestTASK2239_ResolveSessionProxyUnknownProfile verifies unknown
// profile name errors (spec L4028).
func TestTASK2239_ResolveSessionProxyUnknownProfile(t *testing.T) {
	store := NewProxyProfileStore()
	_, err := ResolveSessionProxy(SessionProfileInput{
		ProxyProfile: "nonexistent",
	}, store, nil)
	if err == nil {
		t.Fatal("unknown profile should error")
	}
}

// TestTASK2239_ResolveSessionProxyFallback verifies fallback to
// server-default proxy (spec L4028).
func TestTASK2239_ResolveSessionProxyFallback(t *testing.T) {
	store := NewProxyProfileStore()
	fallback := &ResolvedProxyConfig{
		Source: ProxySourceServerDefault,
		Server: "http://fallback:8080",
	}
	proxy, err := ResolveSessionProxy(SessionProfileInput{}, store, fallback)
	if err != nil {
		t.Fatalf("ResolveSessionProxy: %v", err)
	}
	if proxy.Server != "http://fallback:8080" {
		t.Errorf("server: got %s, want http://fallback:8080", proxy.Server)
	}
}

// TestTASK2239_ResolveSessionProxyRawCredentials verifies raw
// credentials resolution (spec L4028).
func TestTASK2239_ResolveSessionProxyRawCredentials(t *testing.T) {
	store := NewProxyProfileStore()
	proxy, err := ResolveSessionProxy(SessionProfileInput{
		RawProxy: &RawProxyOverride{Host: "raw.proxy", Port: 3128},
	}, store, nil)
	if err != nil {
		t.Fatalf("ResolveSessionProxy: %v", err)
	}
	if proxy.Source != ProxySourceRawCredentials {
		t.Errorf("source: got %s, want raw-credentials", proxy.Source)
	}
	if proxy.Server != "http://raw.proxy:3128" {
		t.Errorf("server: got %s", proxy.Server)
	}
}

// TestTASK2239_ResolveSessionProxyExplicitWins verifies explicit-wins
// mode overrides proxy-implied geo (spec L4028).
func TestTASK2239_ResolveSessionProxyExplicitWins(t *testing.T) {
	store := NewProxyProfileStore()
	store.profiles["de-proxy"] = ProxyProfileConfig{
		Server: "http://proxy.de:8080",
		Locale: "de-DE",
	}
	proxy, err := ResolveSessionProxy(SessionProfileInput{
		ProxyProfile: "de-proxy",
		GeoMode:      GeoModeExplicitWins,
		Locale:       "en-US",
	}, store, nil)
	if err != nil {
		t.Fatalf("ResolveSessionProxy: %v", err)
	}
	if proxy.Locale != "en-US" {
		t.Errorf("locale: got %s, want en-US (explicit override)", proxy.Locale)
	}
}

// TestTASK2239_ResolveSessionProxyLockedRejectsOverrides verifies
// proxy-locked mode rejects explicit overrides (spec L4028).
func TestTASK2239_ResolveSessionProxyLockedRejectsOverrides(t *testing.T) {
	store := NewProxyProfileStore()
	store.profiles["de-proxy"] = ProxyProfileConfig{
		Server: "http://proxy.de:8080",
		Locale: "de-DE",
	}
	_, err := ResolveSessionProxy(SessionProfileInput{
		ProxyProfile: "de-proxy",
		GeoMode:      GeoModeProxyLocked,
		Locale:       "en-US",
	}, store, nil)
	if err == nil {
		t.Fatal("proxy-locked should reject explicit locale override")
	}
}

// TestTASK2239_ResolveSessionProxyLockedRejectsTimezone verifies
// proxy-locked mode rejects timezone overrides (spec L4028).
func TestTASK2239_ResolveSessionProxyLockedRejectsTimezone(t *testing.T) {
	store := NewProxyProfileStore()
	store.profiles["de-proxy"] = ProxyProfileConfig{
		Server: "http://proxy.de:8080",
	}
	_, err := ResolveSessionProxy(SessionProfileInput{
		ProxyProfile: "de-proxy",
		GeoMode:      GeoModeProxyLocked,
		TimezoneID:   "America/New_York",
	}, store, nil)
	if err == nil {
		t.Fatal("proxy-locked should reject explicit timezone override")
	}
}

// TestTASK2239_ResolveSessionProxyLockedRejectsGeo verifies proxy-locked
// mode rejects geolocation overrides (spec L4028).
func TestTASK2239_ResolveSessionProxyLockedRejectsGeo(t *testing.T) {
	store := NewProxyProfileStore()
	store.profiles["de-proxy"] = ProxyProfileConfig{
		Server: "http://proxy.de:8080",
	}
	_, err := ResolveSessionProxy(SessionProfileInput{
		ProxyProfile: "de-proxy",
		GeoMode:      GeoModeProxyLocked,
		Geolocation:  &GeolocationConfig{Latitude: 40.7, Longitude: -74.0},
	}, store, nil)
	if err == nil {
		t.Fatal("proxy-locked should reject explicit geolocation override")
	}
}

// TestTASK2239_ResolveSessionProxyLockedNoOverrides verifies proxy-locked
// mode works when no explicit overrides are given (spec L4028).
func TestTASK2239_ResolveSessionProxyLockedNoOverrides(t *testing.T) {
	store := NewProxyProfileStore()
	store.profiles["de-proxy"] = ProxyProfileConfig{
		Server: "http://proxy.de:8080",
		Locale: "de-DE",
	}
	proxy, err := ResolveSessionProxy(SessionProfileInput{
		ProxyProfile: "de-proxy",
		GeoMode:      GeoModeProxyLocked,
	}, store, nil)
	if err != nil {
		t.Fatalf("ResolveSessionProxy: %v", err)
	}
	if proxy.Locale != "de-DE" {
		t.Errorf("locale: got %s, want de-DE (proxy-implied)", proxy.Locale)
	}
}

// TestTASK2239_ContextHash verifies context hash is deterministic
// and differs for different proxies (spec L4028: contextHash).
func TestTASK2239_ContextHash(t *testing.T) {
	proxy1 := &ResolvedProxyConfig{Server: "http://p1:8080"}
	proxy2 := &ResolvedProxyConfig{Server: "http://p2:8080"}
	h1 := ContextHash(proxy1, GeoModeExplicitWins)
	h2 := ContextHash(proxy1, GeoModeExplicitWins)
	h3 := ContextHash(proxy2, GeoModeExplicitWins)
	if h1 != h2 {
		t.Error("same proxy should produce same hash")
	}
	if h1 == h3 {
		t.Error("different proxies should produce different hashes")
	}
	if len(h1) != 8 {
		t.Errorf("hash length: got %d, want 8", len(h1))
	}
}

// TestTASK2239_ContextHashNilProxy verifies nil proxy is safe.
func TestTASK2239_ContextHashNilProxy(t *testing.T) {
	h := ContextHash(nil, GeoModeExplicitWins)
	if len(h) != 8 {
		t.Errorf("hash length: got %d, want 8", len(h))
	}
}

// TestTASK2239_ValidateProxyProfileInputValid verifies valid input
// passes validation (spec L4028).
func TestTASK2239_ValidateProxyProfileInputValid(t *testing.T) {
	input := SessionProfileInput{
		Locale:      "de-DE",
		Geolocation: &GeolocationConfig{Latitude: 52.52, Longitude: 13.405},
	}
	if err := ValidateProxyProfileInput(input); err != nil {
		t.Errorf("valid input should pass: %v", err)
	}
}

// TestTASK2239_ValidateProxyProfileInputInvalidLat verifies invalid
// latitude fails validation (spec L4028).
func TestTASK2239_ValidateProxyProfileInputInvalidLat(t *testing.T) {
	input := SessionProfileInput{
		Geolocation: &GeolocationConfig{Latitude: 91, Longitude: 0},
	}
	if err := ValidateProxyProfileInput(input); err == nil {
		t.Fatal("invalid latitude should fail validation")
	}
}

// TestTASK2239_ValidateProxyProfileInputInvalidLocale verifies invalid
// locale fails validation (spec L4028: BCP47 regex).
func TestTASK2239_ValidateProxyProfileInputInvalidLocale(t *testing.T) {
	input := SessionProfileInput{
		Locale: "invalid_locale_too_long_12345678901234567890123456789012345",
	}
	if err := ValidateProxyProfileInput(input); err == nil {
		t.Fatal("invalid locale should fail validation")
	}
}

// TestTASK2239_ValidateProxyProfileInputInvalidPort verifies invalid
// port fails validation (spec L4028).
func TestTASK2239_ValidateProxyProfileInputInvalidPort(t *testing.T) {
	input := SessionProfileInput{
		RawProxy: &RawProxyOverride{Host: "proxy", Port: 99999},
	}
	if err := ValidateProxyProfileInput(input); err == nil {
		t.Fatal("invalid port should fail validation")
	}
}

// TestTASK2239_ValidateProxyProfileInputEmptyHost verifies empty host
// fails validation (spec L4028).
func TestTASK2239_ValidateProxyProfileInputEmptyHost(t *testing.T) {
	input := SessionProfileInput{
		RawProxy: &RawProxyOverride{Host: "", Port: 8080},
	}
	if err := ValidateProxyProfileInput(input); err == nil {
		t.Fatal("empty host should fail validation")
	}
}

// TestTASK2239_FullSpecParity verifies full spec parity for L4028
// (spec L4028).
func TestTASK2239_FullSpecParity(t *testing.T) {
	// 1. Cache control
	c := NewBrowserCacheControl("test")
	if !c.Enabled || c.Policy != CachePolicyMaxAge {
		t.Error("cache control default mismatch")
	}
	c.RecordHit(100)
	if c.Stats().HitCount != 1 {
		t.Error("cache hit recording mismatch")
	}

	// 2. Proxy profiles
	store := NewProxyProfileStore()
	store.profiles["de"] = ProxyProfileConfig{
		Server: "http://proxy.de:8080",
		Locale: "de-DE",
	}
	proxy, err := ResolveSessionProxy(SessionProfileInput{
		ProxyProfile: "de",
	}, store, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if proxy.Source != ProxySourceNamedProfile {
		t.Error("proxy source mismatch")
	}

	// 3. Geo modes
	proxy2, err := ResolveSessionProxy(SessionProfileInput{
		ProxyProfile: "de",
		GeoMode:      GeoModeExplicitWins,
		Locale:       "en-US",
	}, store, nil)
	if err != nil {
		t.Fatalf("resolve explicit-wins: %v", err)
	}
	if proxy2.Locale != "en-US" {
		t.Error("explicit-wins should override locale")
	}

	// 4. Context hash
	h1 := ContextHash(proxy, GeoModeExplicitWins)
	h2 := ContextHash(proxy, GeoModeExplicitWins)
	if h1 != h2 {
		t.Error("context hash should be deterministic")
	}

	// 5. Validation
	if err := ValidateProxyProfileInput(SessionProfileInput{
		Geolocation: &GeolocationConfig{Latitude: 91, Longitude: 0},
	}); err == nil {
		t.Error("validation should reject invalid lat")
	}
}
