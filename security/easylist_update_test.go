package security

import (
	"testing"
	"time"
)

func TestEasyListSourcesExist(t *testing.T) {
	if len(EasyListSources) == 0 {
		t.Error("EasyListSources is empty")
	}
	for _, src := range EasyListSources {
		if src == "" {
			t.Error("EasyListSources contains empty URL")
		}
	}
}

func TestDefaultEasyListUpdateConfig(t *testing.T) {
	cfg := DefaultEasyListUpdateConfig()
	if cfg.UpdateInterval != 24*60*60*1e9 { // 24h in ns
		t.Errorf("UpdateInterval = %v, want 24h", cfg.UpdateInterval)
	}
	if cfg.MaxVersions != 3 {
		t.Errorf("MaxVersions = %d, want 3", cfg.MaxVersions)
	}
	if cfg.GradualRolloutPercent != 50 {
		t.Errorf("GradualRolloutPercent = %d, want 50", cfg.GradualRolloutPercent)
	}
	if cfg.GradualRolloutDelay != 1*60*60*1e9 { // 1h in ns
		t.Errorf("GradualRolloutDelay = %v, want 1h", cfg.GradualRolloutDelay)
	}
	if cfg.MaxBreakagesPerHour != 5 {
		t.Errorf("MaxBreakagesPerHour = %d, want 5", cfg.MaxBreakagesPerHour)
	}
	if cfg.MaxSizeMultiplier != 2.0 {
		t.Errorf("MaxSizeMultiplier = %f, want 2.0", cfg.MaxSizeMultiplier)
	}
}

func TestNewEasyListUpdater(t *testing.T) {
	cfg := DefaultEasyListUpdateConfig()
	cfg.StorageDir = t.TempDir() // use temp dir for test
	u := NewEasyListUpdater(cfg)
	if u == nil {
		t.Fatal("NewEasyListUpdater returned nil")
	}
	if u.CurrentVersion() != nil {
		t.Error("CurrentVersion should be nil for new updater")
	}
}

func TestParseABPRules(t *testing.T) {
	content := "! Title: EasyList\n! Homepage: https://easylist.to\n||doubleclick.net^\n||googlesyndication.com^\n\n||ads.example.com^"
	rules := parseABPRules(content)
	if len(rules) != 3 {
		t.Fatalf("rules len = %d, want 3", len(rules))
	}
	if rules[0] != "||doubleclick.net^" {
		t.Errorf("rules[0] = %s", rules[0])
	}
}

func TestRecordBreakageAndRollback(t *testing.T) {
	cfg := DefaultEasyListUpdateConfig()
	cfg.StorageDir = t.TempDir()
	cfg.MaxBreakagesPerHour = 5
	u := NewEasyListUpdater(cfg)

	// Record 5 breakages: should not trigger rollback
	for i := 0; i < 5; i++ {
		u.RecordBreakage()
	}
	if u.ShouldRollback() {
		t.Error("ShouldRollback should be false with 5 breakages (threshold is >5)")
	}

	// Record one more: should trigger rollback
	u.RecordBreakage()
	if !u.ShouldRollback() {
		t.Error("ShouldRollback should be true with 6 breakages (>5 threshold)")
	}
}

func TestGradualRolloutReady(t *testing.T) {
	cfg := DefaultEasyListUpdateConfig()
	cfg.StorageDir = t.TempDir()
	cfg.GradualRolloutDelay = 0 // immediate for test
	u := NewEasyListUpdater(cfg)

	v := &EasyListVersion{DownloadedAt: time.Now().Add(-2 * time.Hour)}
	if !u.GradualRolloutReady(v) {
		t.Error("GradualRolloutReady should be true after delay with no breakages")
	}

	// With breakages, should not be ready
	for i := 0; i < 6; i++ {
		u.RecordBreakage()
	}
	if u.GradualRolloutReady(v) {
		t.Error("GradualRolloutReady should be false with excessive breakages")
	}
}

func TestShouldApplyNowImmediatePhase(t *testing.T) {
	cfg := DefaultEasyListUpdateConfig()
	cfg.StorageDir = t.TempDir()
	u := NewEasyListUpdater(cfg)

	v := &EasyListVersion{DownloadedAt: time.Now()}
	// In immediate phase, 50% threshold
	if !u.ShouldApplyNow(v, 25) {
		t.Error("ShouldApplyNow(25%) should be true in immediate phase (< 50%)")
	}
	if u.ShouldApplyNow(v, 75) {
		t.Error("ShouldApplyNow(75%) should be false in immediate phase (> 50%)")
	}
}
