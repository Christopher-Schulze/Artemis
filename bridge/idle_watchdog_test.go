package bridge

import (
	"sync"
	"testing"
	"time"
)

func TestDefaultIdleWatchdogConfig(t *testing.T) {
	c := DefaultIdleWatchdogConfig
	if !c.Enabled {
		t.Fatal("default should be enabled")
	}
	if c.CheckIntervalSeconds != 60 {
		t.Fatalf("checkInterval=%d want 60", c.CheckIntervalSeconds)
	}
	if c.IdleThresholdSeconds != 1800 {
		t.Fatalf("idleThreshold=%d want 1800", c.IdleThresholdSeconds)
	}
}

func TestNewIdleWatchdogDefaults(t *testing.T) {
	w := NewIdleWatchdog(IdleWatchdogConfig{Enabled: true})
	c := w.Config()
	if c.CheckIntervalSeconds != 60 {
		t.Fatalf("checkInterval=%d want 60", c.CheckIntervalSeconds)
	}
	if c.IdleThresholdSeconds != 1800 {
		t.Fatalf("idleThreshold=%d want 1800", c.IdleThresholdSeconds)
	}
}

func TestIdleWatchdogRegister(t *testing.T) {
	w := NewIdleWatchdog(DefaultIdleWatchdogConfig)
	w.RegisterWorkspace("ws1")
	st, ok := w.GetWorkspaceState("ws1")
	if !ok {
		t.Fatal("workspace should be registered")
	}
	if st.WorkspaceID != "ws1" {
		t.Fatalf("workspaceID=%q want ws1", st.WorkspaceID)
	}
	if st.AutoStopped {
		t.Fatal("new workspace should not be auto-stopped")
	}
	if st.LastActive.IsZero() {
		t.Fatal("lastActive should be set on register")
	}
	if s := w.Stats(); s.TotalRegistered != 1 {
		t.Fatalf("totalRegistered=%d want 1", s.TotalRegistered)
	}
}

func TestIdleWatchdogRegisterEmptyID(t *testing.T) {
	w := NewIdleWatchdog(DefaultIdleWatchdogConfig)
	w.RegisterWorkspace("")
	if _, ok := w.GetWorkspaceState(""); ok {
		t.Fatal("empty id should not be registered")
	}
}

func TestIdleWatchdogRegisterDuplicate(t *testing.T) {
	w := NewIdleWatchdog(DefaultIdleWatchdogConfig)
	w.RegisterWorkspace("ws1")
	st1, _ := w.GetWorkspaceState("ws1")
	w.RegisterWorkspace("ws1") // no-op
	st2, _ := w.GetWorkspaceState("ws1")
	if !st1.LastActive.Equal(st2.LastActive) {
		t.Fatal("re-register should preserve existing state")
	}
	if s := w.Stats(); s.TotalRegistered != 1 {
		t.Fatalf("totalRegistered=%d want 1 (duplicate not counted)", s.TotalRegistered)
	}
}

func TestIdleWatchdogRecordActivity(t *testing.T) {
	w := NewIdleWatchdog(DefaultIdleWatchdogConfig)
	w.RegisterWorkspace("ws1")
	st1, _ := w.GetWorkspaceState("ws1")
	time.Sleep(2 * time.Millisecond)
	w.RecordActivity("ws1")
	st2, _ := w.GetWorkspaceState("ws1")
	if !st2.LastActive.After(st1.LastActive) {
		t.Fatal("recordActivity should update lastActive")
	}
}

func TestIdleWatchdogRecordActivityUnregistered(t *testing.T) {
	w := NewIdleWatchdog(DefaultIdleWatchdogConfig)
	w.RecordActivity("nope")
	if _, ok := w.GetWorkspaceState("nope"); ok {
		t.Fatal("recordActivity on unregistered should not register")
	}
}

func TestIdleWatchdogCheckIdleNone(t *testing.T) {
	w := NewIdleWatchdog(IdleWatchdogConfig{Enabled: true, CheckIntervalSeconds: 60, IdleThresholdSeconds: 1800})
	w.RegisterWorkspace("ws1")
	if idle := w.CheckIdle(); len(idle) != 0 {
		t.Fatalf("idle=%v want none", idle)
	}
	if s := w.Stats(); s.TotalChecks != 1 {
		t.Fatalf("totalChecks=%d want 1", s.TotalChecks)
	}
}

func TestIdleWatchdogCheckIdleExceeded(t *testing.T) {
	w := NewIdleWatchdog(IdleWatchdogConfig{Enabled: true, CheckIntervalSeconds: 60, IdleThresholdSeconds: 10})
	frozen := time.Now()
	w.SetNow(func() time.Time { return frozen })
	w.RegisterWorkspace("ws1")
	// Advance clock past the 10s threshold.
	w.SetNow(func() time.Time { return frozen.Add(15 * time.Second) })
	idle := w.CheckIdle()
	if len(idle) != 1 || idle[0] != "ws1" {
		t.Fatalf("idle=%v want [ws1]", idle)
	}
	// CheckIdle must not auto-stop.
	if w.IsAutoStopped("ws1") {
		t.Fatal("CheckIdle should not auto-stop")
	}
}

func TestIdleWatchdogCheckIdleDisabled(t *testing.T) {
	w := NewIdleWatchdog(IdleWatchdogConfig{Enabled: false, CheckIntervalSeconds: 60, IdleThresholdSeconds: 1})
	w.RegisterWorkspace("ws1")
	time.Sleep(5 * time.Millisecond)
	if idle := w.CheckIdle(); len(idle) != 0 {
		t.Fatalf("disabled should return no idle: %v", idle)
	}
}

func TestIdleWatchdogAutoStop(t *testing.T) {
	w := NewIdleWatchdog(IdleWatchdogConfig{Enabled: true, CheckIntervalSeconds: 60, IdleThresholdSeconds: 10})
	frozen := time.Now()
	w.SetNow(func() time.Time { return frozen })
	w.RegisterWorkspace("ws1")
	w.SetNow(func() time.Time { return frozen.Add(15 * time.Second) })
	stopped := w.AutoStop()
	if len(stopped) != 1 || stopped[0] != "ws1" {
		t.Fatalf("stopped=%v want [ws1]", stopped)
	}
	if !w.IsAutoStopped("ws1") {
		t.Fatal("ws1 should be auto-stopped")
	}
	if s := w.Stats(); s.TotalAutoStopped != 1 {
		t.Fatalf("totalAutoStopped=%d want 1", s.TotalAutoStopped)
	}
}

func TestIdleWatchdogAutoStopSkipsAlreadyStopped(t *testing.T) {
	w := NewIdleWatchdog(IdleWatchdogConfig{Enabled: true, CheckIntervalSeconds: 60, IdleThresholdSeconds: 10})
	frozen := time.Now()
	w.SetNow(func() time.Time { return frozen })
	w.RegisterWorkspace("ws1")
	w.SetNow(func() time.Time { return frozen.Add(15 * time.Second) })
	if stopped := w.AutoStop(); len(stopped) != 1 {
		t.Fatalf("first autostop=%v want 1", stopped)
	}
	// Second call should not re-stop.
	if stopped := w.AutoStop(); len(stopped) != 0 {
		t.Fatalf("second autostop=%v want none", stopped)
	}
	if s := w.Stats(); s.TotalAutoStopped != 1 {
		t.Fatalf("totalAutoStopped=%d want 1", s.TotalAutoStopped)
	}
}

func TestIdleWatchdogAutoStopNotIdle(t *testing.T) {
	w := NewIdleWatchdog(IdleWatchdogConfig{Enabled: true, CheckIntervalSeconds: 60, IdleThresholdSeconds: 10})
	w.RegisterWorkspace("ws1")
	if stopped := w.AutoStop(); len(stopped) != 0 {
		t.Fatalf("stopped=%v want none (not idle)", stopped)
	}
	if w.IsAutoStopped("ws1") {
		t.Fatal("active workspace should not be auto-stopped")
	}
}

func TestIdleWatchdogGetWorkspaceStateMissing(t *testing.T) {
	w := NewIdleWatchdog(DefaultIdleWatchdogConfig)
	if _, ok := w.GetWorkspaceState("nope"); ok {
		t.Fatal("missing workspace should return false")
	}
}

func TestIdleWatchdogGetWorkspaceStateCopy(t *testing.T) {
	w := NewIdleWatchdog(DefaultIdleWatchdogConfig)
	w.RegisterWorkspace("ws1")
	st, _ := w.GetWorkspaceState("ws1")
	st.AutoStopped = true
	if w.IsAutoStopped("ws1") {
		t.Fatal("mutation of returned copy should not affect watchdog")
	}
}

func TestIdleWatchdogGetActiveWorkspaces(t *testing.T) {
	w := NewIdleWatchdog(IdleWatchdogConfig{Enabled: true, CheckIntervalSeconds: 60, IdleThresholdSeconds: 10})
	frozen := time.Now()
	w.SetNow(func() time.Time { return frozen })
	w.RegisterWorkspace("ws1")
	// Advance clock past the threshold, then register ws2 so it is fresh.
	w.SetNow(func() time.Time { return frozen.Add(15 * time.Second) })
	w.RegisterWorkspace("ws2")
	_ = w.AutoStop()
	active := w.GetActiveWorkspaces()
	if len(active) != 1 || active[0] != "ws2" {
		t.Fatalf("active=%v want [ws2]", active)
	}
}

func TestIdleWatchdogGetActiveWorkspacesAllActive(t *testing.T) {
	w := NewIdleWatchdog(DefaultIdleWatchdogConfig)
	w.RegisterWorkspace("ws1")
	w.RegisterWorkspace("ws2")
	if active := w.GetActiveWorkspaces(); len(active) != 2 {
		t.Fatalf("active=%d want 2", len(active))
	}
}

func TestIdleWatchdogIsAutoStoppedUnregistered(t *testing.T) {
	w := NewIdleWatchdog(DefaultIdleWatchdogConfig)
	if w.IsAutoStopped("nope") {
		t.Fatal("unregistered should not be auto-stopped")
	}
}

func TestIdleWatchdogReset(t *testing.T) {
	w := NewIdleWatchdog(IdleWatchdogConfig{Enabled: true, CheckIntervalSeconds: 60, IdleThresholdSeconds: 10})
	frozen := time.Now()
	w.SetNow(func() time.Time { return frozen })
	w.RegisterWorkspace("ws1")
	w.SetNow(func() time.Time { return frozen.Add(15 * time.Second) })
	_ = w.AutoStop()
	if !w.IsAutoStopped("ws1") {
		t.Fatal("ws1 should be auto-stopped before reset")
	}
	later := frozen.Add(20 * time.Second)
	w.SetNow(func() time.Time { return later })
	if !w.Reset("ws1") {
		t.Fatal("reset should return true for registered workspace")
	}
	if w.IsAutoStopped("ws1") {
		t.Fatal("ws1 should not be auto-stopped after reset")
	}
	st, _ := w.GetWorkspaceState("ws1")
	if !st.LastActive.Equal(later) {
		t.Fatal("reset should update lastActive")
	}
	// After reset, workspace should appear in active list and not be re-stopped immediately.
	if active := w.GetActiveWorkspaces(); len(active) != 1 {
		t.Fatalf("active=%v want [ws1]", active)
	}
	if stopped := w.AutoStop(); len(stopped) != 0 {
		t.Fatalf("stopped=%v want none after reset", stopped)
	}
}

func TestIdleWatchdogResetUnregistered(t *testing.T) {
	w := NewIdleWatchdog(DefaultIdleWatchdogConfig)
	if w.Reset("nope") {
		t.Fatal("reset should return false for unregistered workspace")
	}
}

func TestIdleWatchdogUnregister(t *testing.T) {
	w := NewIdleWatchdog(DefaultIdleWatchdogConfig)
	w.RegisterWorkspace("ws1")
	if !w.UnregisterWorkspace("ws1") {
		t.Fatal("unregister should return true for registered workspace")
	}
	if _, ok := w.GetWorkspaceState("ws1"); ok {
		t.Fatal("workspace should be removed after unregister")
	}
}

func TestIdleWatchdogUnregisterMissing(t *testing.T) {
	w := NewIdleWatchdog(DefaultIdleWatchdogConfig)
	if w.UnregisterWorkspace("nope") {
		t.Fatal("unregister should return false for missing workspace")
	}
}

func TestIdleWatchdogStats(t *testing.T) {
	w := NewIdleWatchdog(IdleWatchdogConfig{Enabled: true, CheckIntervalSeconds: 60, IdleThresholdSeconds: 10})
	frozen := time.Now()
	w.SetNow(func() time.Time { return frozen })
	w.RegisterWorkspace("ws1")
	w.RegisterWorkspace("ws2")
	w.SetNow(func() time.Time { return frozen.Add(15 * time.Second) })
	_ = w.CheckIdle()
	_ = w.AutoStop()
	s := w.Stats()
	if s.TotalRegistered != 2 {
		t.Fatalf("totalRegistered=%d want 2", s.TotalRegistered)
	}
	if s.TotalChecks != 1 {
		t.Fatalf("totalChecks=%d want 1", s.TotalChecks)
	}
	if s.TotalAutoStopped != 2 {
		t.Fatalf("totalAutoStopped=%d want 2", s.TotalAutoStopped)
	}
}

func TestIdleWatchdogConcurrent(t *testing.T) {
	w := NewIdleWatchdog(DefaultIdleWatchdogConfig)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "ws" + string(rune('0'+i%5))
			w.RegisterWorkspace(id)
			w.RecordActivity(id)
			_ = w.CheckIdle()
			_ = w.AutoStop()
			_ = w.GetActiveWorkspaces()
			_ = w.IsAutoStopped(id)
			_, _ = w.GetWorkspaceState(id)
		}(i)
	}
	wg.Wait()
	s := w.Stats()
	if s.TotalChecks == 0 {
		t.Fatal("expected some checks under concurrency")
	}
}
