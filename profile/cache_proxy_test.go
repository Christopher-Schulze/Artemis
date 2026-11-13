package profile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ==================== cache.go tests ====================

// TestCacheControlCreation verifies NewBrowserCacheControl
// creates a control with default settings.
func TestCacheControlCreation(t *testing.T) {
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

// TestCacheRecordHitMiss verifies hit/miss recording
// (cache metrics).
func TestCacheRecordHitMiss(t *testing.T) {
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

// TestCacheHitRate verifies hit rate calculation
// (cache metrics).
func TestCacheHitRate(t *testing.T) {
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

// TestCacheHitRateEmpty verifies hit rate is 0 for empty cache.
func TestCacheHitRateEmpty(t *testing.T) {
	c := NewBrowserCacheControl("p")
	if c.HitRate() != 0 {
		t.Error("empty cache hit rate should be 0")
	}
}

// TestCacheClear verifies Clear resets the cache
// (clear-cache).
func TestCacheClear(t *testing.T) {
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

// TestCacheEviction verifies eviction recording
// (cache metrics).
func TestCacheEviction(t *testing.T) {
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

// TestCacheEvictionNotNegative verifies bytes don't go negative.
func TestCacheEvictionNotNegative(t *testing.T) {
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

// TestCacheIsExpiredMaxAge verifies max-age expiration
// (max-age enforcement).
func TestCacheIsExpiredMaxAge(t *testing.T) {
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

// TestCacheIsExpiredDisabled verifies disabled cache expires
// everything.
func TestCacheIsExpiredDisabled(t *testing.T) {
	c := NewBrowserCacheControl("p")
	c.SetPolicy(CachePolicyDisabled, 0)
	if !c.IsExpired(time.Now()) {
		t.Error("disabled cache should expire everything")
	}
}

// TestCacheIsExpiredDefault verifies default policy never
// expires.
func TestCacheIsExpiredDefault(t *testing.T) {
	c := NewBrowserCacheControl("p")
	c.SetPolicy(CachePolicyDefault, 0)
	if c.IsExpired(time.Now().Add(-100 * time.Hour)) {
		t.Error("default policy should never expire")
	}
}

// TestCacheShouldEvict verifies max-size eviction logic
// (cache size management).
func TestCacheShouldEvict(t *testing.T) {
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

// TestCacheShouldEvictNoMaxSize verifies no max size means
// no eviction.
func TestCacheShouldEvictNoMaxSize(t *testing.T) {
	c := NewBrowserCacheControl("p")
	c.RecordHit(999999999)
	if c.ShouldEvict(999999999) {
		t.Error("no max size should never evict")
	}
}

// TestCacheSetEnabled verifies enabling/disabling cache.
func TestCacheSetEnabled(t *testing.T) {
	c := NewBrowserCacheControl("p")
	c.SetEnabled(false)
	if c.Enabled {
		t.Error("cache should be disabled")
	}
	if !c.IsExpired(time.Now()) {
		t.Error("disabled cache should expire everything")
	}
}

// TestCacheNilSafe verifies nil cache control is safe.
func TestCacheNilSafe(t *testing.T) {
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

// TestCacheString verifies the String method.
func TestCacheString(t *testing.T) {
	c := NewBrowserCacheControl("p")
	s := c.String()
	if s == "" {
		t.Error("String should not be empty")
	}
}

// TestCacheControlManagerGet verifies the manager creates
// and returns per-profile controls (per-profile isolation).
func TestCacheControlManagerGet(t *testing.T) {
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

// TestCacheControlManagerClearAll verifies ClearAll clears
// all profiles.
func TestCacheControlManagerClearAll(t *testing.T) {
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

// TestCacheControlManagerAllStats verifies AllStats returns
// stats for all profiles.
func TestCacheControlManagerAllStats(t *testing.T) {
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

// TestCacheControlManagerNilSafe verifies nil manager is safe.
func TestCacheControlManagerNilSafe(t *testing.T) {
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

// TestGeoModeConstants verifies the geo mode constants.
func TestGeoModeConstants(t *testing.T) {
	if GeoModeExplicitWins != "explicit_wins" {
		t.Error("explicit_wins constant mismatch")
	}
	if GeoModeProxyLocked != "proxy_locked" {
		t.Error("proxy_locked constant mismatch")
	}
}

// TestProxyProfileStoreLoad verifies loading proxy profiles
// from JSON.
func TestProxyProfileStoreLoad(t *testing.T) {
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
	if err := os.WriteFile(path, []byte(jsonContent), 0o600); err != nil {
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

// TestProxyProfileStoreCaseInsensitive verifies case-insensitive
// name lookup.
func TestProxyProfileStoreCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy-profiles.json")
	jsonContent := `{"MyProxy": {"server": "http://proxy:8080"}}`
	if err := os.WriteFile(path, []byte(jsonContent), 0o600); err != nil {
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

// TestProxyProfileStoreEmptyPath verifies empty path returns
// empty store.
func TestProxyProfileStoreEmptyPath(t *testing.T) {
	store, err := LoadProxyProfiles("")
	if err != nil {
		t.Fatalf("empty path should not error: %v", err)
	}
	if len(store.Names()) != 0 {
		t.Error("empty path should produce empty store")
	}
}

// TestProxyProfileStoreValidationLat verifies latitude
// validation (lat∈[-90,90]).
func TestProxyProfileStoreValidationLat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy-profiles.json")
	jsonContent := `{"bad": {"server": "http://p:80", "geolocation": {"latitude": 91, "longitude": 0}}}`
	if err := os.WriteFile(path, []byte(jsonContent), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProxyProfiles(path)
	if err == nil {
		t.Fatal("latitude > 90 should fail validation")
	}
}

// TestProxyProfileStoreValidationLong verifies longitude
// validation (long∈[-180,180]).
func TestProxyProfileStoreValidationLong(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy-profiles.json")
	jsonContent := `{"bad": {"server": "http://p:80", "geolocation": {"latitude": 0, "longitude": 181}}}`
	if err := os.WriteFile(path, []byte(jsonContent), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProxyProfiles(path)
	if err == nil {
		t.Fatal("longitude > 180 should fail validation")
	}
}

// TestProxyProfileStoreValidationLocale verifies locale
// validation (BCP47 regex).
func TestProxyProfileStoreValidationLocale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy-profiles.json")
	jsonContent := `{"bad": {"server": "http://p:80", "locale": "invalid_locale_1234567890123456789012345678901234567890"}}`
	if err := os.WriteFile(path, []byte(jsonContent), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProxyProfiles(path)
	if err == nil {
		t.Fatal("invalid locale should fail validation")
	}
}

// TestProxyProfileStoreValidationEmptyServer verifies empty
// server is rejected.
func TestProxyProfileStoreValidationEmptyServer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "proxy-profiles.json")
	jsonContent := `{"bad": {"server": ""}}`
	if err := os.WriteFile(path, []byte(jsonContent), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadProxyProfiles(path)
	if err == nil {
		t.Fatal("empty server should fail validation")
	}
}

// TestGetConfiguredServerProxy verifies the fallback server
// proxy resolution (research getConfiguredServerProxy).
func TestGetConfiguredServerProxy(t *testing.T) {
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

// TestGetConfiguredServerProxyEmpty verifies empty config
// returns nil.
func TestGetConfiguredServerProxyEmpty(t *testing.T) {
	if GetConfiguredServerProxy(ConfiguredServerProxy{}) != nil {
		t.Error("empty config should return nil")
	}
}

// TestNormalizeRawProxy verifies raw proxy normalization
// (research normalizeRawProxy).
func TestNormalizeRawProxy(t *testing.T) {
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

// TestNormalizeRawProxyEmpty verifies empty raw proxy errors.
func TestNormalizeRawProxyEmpty(t *testing.T) {
	_, err := NormalizeRawProxy(RawProxyOverride{})
	if err == nil {
		t.Fatal("empty raw proxy should error")
	}
}

// TestResolveSessionProxyNamedProfile verifies named profile
// resolution.
func TestResolveSessionProxyNamedProfile(t *testing.T) {
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

// TestResolveSessionProxyUnknownProfile verifies unknown
// profile name errors.
func TestResolveSessionProxyUnknownProfile(t *testing.T) {
	store := NewProxyProfileStore()
	_, err := ResolveSessionProxy(SessionProfileInput{
		ProxyProfile: "nonexistent",
	}, store, nil)
	if err == nil {
		t.Fatal("unknown profile should error")
	}
}

// TestResolveSessionProxyFallback verifies fallback to
// server-default proxy.
func TestResolveSessionProxyFallback(t *testing.T) {
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

// TestResolveSessionProxyRawCredentials verifies raw
// credentials resolution.
func TestResolveSessionProxyRawCredentials(t *testing.T) {
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

// TestResolveSessionProxyExplicitWins verifies explicit-wins
// mode overrides proxy-implied geo.
func TestResolveSessionProxyExplicitWins(t *testing.T) {
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

// TestResolveSessionProxyLockedRejectsOverrides verifies
// proxy-locked mode rejects explicit overrides.
func TestResolveSessionProxyLockedRejectsOverrides(t *testing.T) {
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

// TestResolveSessionProxyLockedRejectsTimezone verifies
// proxy-locked mode rejects timezone overrides.
func TestResolveSessionProxyLockedRejectsTimezone(t *testing.T) {
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

// TestResolveSessionProxyLockedRejectsGeo verifies proxy-locked
// mode rejects geolocation overrides.
func TestResolveSessionProxyLockedRejectsGeo(t *testing.T) {
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

// TestResolveSessionProxyLockedNoOverrides verifies proxy-locked
// mode works when no explicit overrides are given.
func TestResolveSessionProxyLockedNoOverrides(t *testing.T) {
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

// TestContextHash verifies context hash is deterministic
// and differs for different proxies (contextHash).
func TestContextHash(t *testing.T) {
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

// TestContextHashNilProxy verifies nil proxy is safe.
func TestContextHashNilProxy(t *testing.T) {
	h := ContextHash(nil, GeoModeExplicitWins)
	if len(h) != 8 {
		t.Errorf("hash length: got %d, want 8", len(h))
	}
}

// TestValidateProxyProfileInputValid verifies valid input
// passes validation.
func TestValidateProxyProfileInputValid(t *testing.T) {
	input := SessionProfileInput{
		Locale:      "de-DE",
		Geolocation: &GeolocationConfig{Latitude: 52.52, Longitude: 13.405},
	}
	if err := ValidateProxyProfileInput(input); err != nil {
		t.Errorf("valid input should pass: %v", err)
	}
}

// TestValidateProxyProfileInputInvalidLat verifies invalid
// latitude fails validation.
func TestValidateProxyProfileInputInvalidLat(t *testing.T) {
	input := SessionProfileInput{
		Geolocation: &GeolocationConfig{Latitude: 91, Longitude: 0},
	}
	if err := ValidateProxyProfileInput(input); err == nil {
		t.Fatal("invalid latitude should fail validation")
	}
}

// TestValidateProxyProfileInputInvalidLocale verifies invalid
// locale fails validation (BCP47 regex).
func TestValidateProxyProfileInputInvalidLocale(t *testing.T) {
	input := SessionProfileInput{
		Locale: "invalid_locale_too_long_12345678901234567890123456789012345",
	}
	if err := ValidateProxyProfileInput(input); err == nil {
		t.Fatal("invalid locale should fail validation")
	}
}

// TestValidateProxyProfileInputInvalidPort verifies invalid
// port fails validation.
func TestValidateProxyProfileInputInvalidPort(t *testing.T) {
	input := SessionProfileInput{
		RawProxy: &RawProxyOverride{Host: "proxy", Port: 99999},
	}
	if err := ValidateProxyProfileInput(input); err == nil {
		t.Fatal("invalid port should fail validation")
	}
}

// TestValidateProxyProfileInputEmptyHost verifies empty host
// fails validation.
func TestValidateProxyProfileInputEmptyHost(t *testing.T) {
	input := SessionProfileInput{
		RawProxy: &RawProxyOverride{Host: "", Port: 8080},
	}
	if err := ValidateProxyProfileInput(input); err == nil {
		t.Fatal("empty host should fail validation")
	}
}

// TestCacheProxyParity verifies full parity.
func TestCacheProxyParity(t *testing.T) {
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
