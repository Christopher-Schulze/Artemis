package bridge

import (
	"testing"
	"time"
)

func TestDefaultInactivityConfig(t *testing.T) {
	c := DefaultInactivityConfig
	if c.IdleTimeout != 5*time.Minute {
		t.Fatalf("idleTimeout=%v", c.IdleTimeout)
	}
	if c.CheckInterval != 30*time.Second {
		t.Fatalf("checkInterval=%v", c.CheckInterval)
	}
	if !c.Enabled {
		t.Fatal("should be enabled")
	}
}

func TestRegisterActivity(t *testing.T) {
	m := NewInactivityMonitor(DefaultInactivityConfig)
	m.RegisterActivity("s1")
	if m.GetActiveSessions() != 1 {
		t.Fatalf("count=%d", m.GetActiveSessions())
	}
}

func TestRegisterActivityUpdatesTimestamp(t *testing.T) {
	m := NewInactivityMonitor(DefaultInactivityConfig)
	m.RegisterActivity("s1")
	first := m.GetSessionActivity("s1").LastActive
	time.Sleep(2 * time.Millisecond)
	m.RegisterActivity("s1")
	second := m.GetSessionActivity("s1").LastActive
	if !second.After(first) {
		t.Fatal("timestamp should update on re-register")
	}
}

func TestRegisterActivityEmptyID(t *testing.T) {
	m := NewInactivityMonitor(DefaultInactivityConfig)
	m.RegisterActivity("")
	if m.GetActiveSessions() != 0 {
		t.Fatal("empty id should be ignored")
	}
}

func TestCheckInactiveNone(t *testing.T) {
	m := NewInactivityMonitor(DefaultInactivityConfig)
	m.RegisterActivity("s1")
	if expired := m.CheckInactive(); len(expired) != 0 {
		t.Fatalf("expired=%v", expired)
	}
}

func TestCheckInactiveExpired(t *testing.T) {
	m := NewInactivityMonitor(InactivityConfig{IdleTimeout: 10 * time.Millisecond, CheckInterval: 5 * time.Millisecond, Enabled: true})
	m.RegisterActivity("s1")
	time.Sleep(20 * time.Millisecond)
	expired := m.CheckInactive()
	if len(expired) != 1 || expired[0] != "s1" {
		t.Fatalf("expired=%v", expired)
	}
}

func TestCheckInactiveDisabled(t *testing.T) {
	m := NewInactivityMonitor(InactivityConfig{IdleTimeout: time.Millisecond, CheckInterval: time.Millisecond, Enabled: false})
	m.RegisterActivity("s1")
	time.Sleep(5 * time.Millisecond)
	if expired := m.CheckInactive(); len(expired) != 0 {
		t.Fatalf("disabled should return no expired: %v", expired)
	}
}

func TestCleanupExpired(t *testing.T) {
	m := NewInactivityMonitor(InactivityConfig{IdleTimeout: 10 * time.Millisecond, CheckInterval: 5 * time.Millisecond, Enabled: true})
	m.RegisterActivity("s1")
	m.RegisterActivity("s2")
	time.Sleep(20 * time.Millisecond)
	count := m.CleanupExpired()
	if count != 2 {
		t.Fatalf("count=%d", count)
	}
	if m.GetActiveSessions() != 0 {
		t.Fatalf("remaining=%d", m.GetActiveSessions())
	}
}

func TestCleanupExpiredNone(t *testing.T) {
	m := NewInactivityMonitor(DefaultInactivityConfig)
	m.RegisterActivity("s1")
	if count := m.CleanupExpired(); count != 0 {
		t.Fatalf("count=%d", count)
	}
}

func TestGetActiveSessions(t *testing.T) {
	m := NewInactivityMonitor(DefaultInactivityConfig)
	for i := 0; i < 5; i++ {
		m.RegisterActivity("s" + string(rune('0'+i)))
	}
	if m.GetActiveSessions() != 5 {
		t.Fatalf("count=%d", m.GetActiveSessions())
	}
}

func TestReapOrphans(t *testing.T) {
	m := NewInactivityMonitor(DefaultInactivityConfig)
	m.RegisterActivity("s1")
	m.RegisterActivity("s2")
	m.RegisterActivity("s3")
	count := m.ReapOrphans([]string{"s1", "s3"})
	if count != 1 {
		t.Fatalf("reaped=%d want 1", count)
	}
	if m.GetActiveSessions() != 2 {
		t.Fatalf("remaining=%d", m.GetActiveSessions())
	}
	if m.GetSessionActivity("s2") != nil {
		t.Fatal("s2 should be reaped")
	}
}

func TestReapOrphansNoneKnown(t *testing.T) {
	m := NewInactivityMonitor(DefaultInactivityConfig)
	m.RegisterActivity("s1")
	if count := m.ReapOrphans(nil); count != 1 {
		t.Fatalf("count=%d", count)
	}
}

func TestReapOrphansAllKnown(t *testing.T) {
	m := NewInactivityMonitor(DefaultInactivityConfig)
	m.RegisterActivity("s1")
	if count := m.ReapOrphans([]string{"s1"}); count != 0 {
		t.Fatalf("count=%d", count)
	}
}

func TestForceExpire(t *testing.T) {
	m := NewInactivityMonitor(DefaultInactivityConfig)
	m.RegisterActivity("s1")
	m.ForceExpire("s1")
	expired := m.CheckInactive()
	if len(expired) != 1 || expired[0] != "s1" {
		t.Fatalf("expired=%v", expired)
	}
}

func TestGetSessionActivityMissing(t *testing.T) {
	m := NewInactivityMonitor(DefaultInactivityConfig)
	if m.GetSessionActivity("nope") != nil {
		t.Fatal("missing session should return nil")
	}
}

func TestGetSessionActivityCopy(t *testing.T) {
	m := NewInactivityMonitor(DefaultInactivityConfig)
	m.RegisterActivity("s1")
	sa := m.GetSessionActivity("s1")
	sa.LastActive = time.Time{}
	if m.GetSessionActivity("s1").LastActive.IsZero() {
		t.Fatal("mutation of returned copy should not affect monitor")
	}
}

func TestNewInactivityMonitorDefaults(t *testing.T) {
	m := NewInactivityMonitor(InactivityConfig{Enabled: true})
	cfg := m.Config()
	if cfg.IdleTimeout != DefaultInactivityConfig.IdleTimeout {
		t.Fatalf("idleTimeout=%v", cfg.IdleTimeout)
	}
	if cfg.CheckInterval != DefaultInactivityConfig.CheckInterval {
		t.Fatalf("checkInterval=%v", cfg.CheckInterval)
	}
}

func TestSetNow(t *testing.T) {
	m := NewInactivityMonitor(DefaultInactivityConfig)
	frozen := time.Now()
	m.SetNow(func() time.Time { return frozen })
	m.RegisterActivity("s1")
	if !m.GetSessionActivity("s1").LastActive.Equal(frozen) {
		t.Fatal("custom clock not used")
	}
}
