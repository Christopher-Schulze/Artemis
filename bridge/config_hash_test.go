package bridge

import (
	"strings"
	"testing"
	"time"
)

func TestComputeConfigHashDeterministic(t *testing.T) {
	input := ConfigHashInput{
		CdpPort: 9222, Headless: true, SecurityEpoch: "v1",
		WorkspaceDir: "/tmp/ws", MountFormatVersion: 1,
	}
	h1 := ComputeConfigHash(input)
	h2 := ComputeConfigHash(input)
	if h1 != h2 {
		t.Fatalf("hash should be deterministic: %s != %s", h1, h2)
	}
}

func TestComputeConfigHashChangesOnInput(t *testing.T) {
	input1 := ConfigHashInput{CdpPort: 9222, Headless: true}
	input2 := ConfigHashInput{CdpPort: 9223, Headless: true}
	if ComputeConfigHash(input1) == ComputeConfigHash(input2) {
		t.Fatal("hash should differ when input differs")
	}
}

func TestComputeConfigHashLength(t *testing.T) {
	h := ComputeConfigHash(ConfigHashInput{})
	if len(h) != 64 {
		t.Fatalf("hash length=%d want 64 (SHA-256 hex)", len(h))
	}
}

func TestConfigHashRegistryRegisterAndGet(t *testing.T) {
	r := NewConfigHashRegistry()
	meta := BrowserBridgeMetadata{
		ContainerName: "browser-1",
		ConfigHash:    "abc123",
		Running:       true,
	}
	r.RegisterBridge("scope1", meta)
	got, ok := r.GetBridge("scope1")
	if !ok {
		t.Fatal("bridge should be registered")
	}
	if got.ContainerName != "browser-1" {
		t.Fatalf("container=%q", got.ContainerName)
	}
}

func TestConfigHashRegistryGetMissing(t *testing.T) {
	r := NewConfigHashRegistry()
	if _, ok := r.GetBridge("nope"); ok {
		t.Fatal("missing bridge should return false")
	}
}

func TestConfigHashRegistryRemove(t *testing.T) {
	r := NewConfigHashRegistry()
	r.RegisterBridge("scope1", BrowserBridgeMetadata{ContainerName: "c1"})
	if !r.RemoveBridge("scope1") {
		t.Fatal("remove should return true")
	}
	if _, ok := r.GetBridge("scope1"); ok {
		t.Fatal("bridge should be gone")
	}
	if r.RemoveBridge("scope1") {
		t.Fatal("second remove should return false")
	}
}

func TestConfigHashRegistryCheckHashMatched(t *testing.T) {
	r := NewConfigHashRegistry()
	r.RegisterBridge("scope1", BrowserBridgeMetadata{
		ContainerName: "c1",
		ConfigHash:    "hash123",
		Running:       true,
	})
	matched, isHot, warning := r.CheckHash("scope1", "hash123")
	if !matched || isHot || warning != "" {
		t.Fatalf("matched=%v isHot=%v warning=%q", matched, isHot, warning)
	}
}

func TestConfigHashRegistryCheckHashMismatchHot(t *testing.T) {
	r := NewConfigHashRegistry()
	r.RegisterBridge("scope1", BrowserBridgeMetadata{
		ContainerName: "c1",
		ConfigHash:    "old-hash",
		Running:       true,
	})
	matched, isHot, warning := r.CheckHash("scope1", "new-hash")
	if matched {
		t.Fatal("should not match")
	}
	if !isHot {
		t.Fatal("should be hot (recently registered)")
	}
	if warning == "" {
		t.Fatal("should have warning")
	}
	if !strings.Contains(warning, "c1") {
		t.Fatalf("warning=%q", warning)
	}
}

func TestConfigHashRegistryCheckHashMismatchCold(t *testing.T) {
	r := NewConfigHashRegistry()
	r.RegisterBridge("scope1", BrowserBridgeMetadata{
		ContainerName: "c1",
		ConfigHash:    "old-hash",
		Running:       true,
	})
	// Simulate cold by setting LastUsedAt to the past.
	r.mu.Lock()
	meta := r.bridges["scope1"]
	meta.LastUsedAt = time.Now().Add(-10 * time.Minute)
	r.bridges["scope1"] = meta
	r.mu.Unlock()

	matched, isHot, warning := r.CheckHash("scope1", "new-hash")
	if matched {
		t.Fatal("should not match")
	}
	if isHot {
		t.Fatal("should be cold (10 min ago)")
	}
	if warning != "" {
		t.Fatalf("warning should be empty when cold, got %q", warning)
	}
}

func TestConfigHashRegistryShouldRecreateCold(t *testing.T) {
	r := NewConfigHashRegistry()
	r.RegisterBridge("scope1", BrowserBridgeMetadata{
		ContainerName: "c1",
		ConfigHash:    "old",
		Running:       true,
	})
	r.mu.Lock()
	meta := r.bridges["scope1"]
	meta.LastUsedAt = time.Now().Add(-10 * time.Minute)
	r.bridges["scope1"] = meta
	r.mu.Unlock()

	if !r.ShouldRecreate("scope1", "new") {
		t.Fatal("should recreate when cold mismatch")
	}
}

func TestConfigHashRegistryShouldRecreateHot(t *testing.T) {
	r := NewConfigHashRegistry()
	r.RegisterBridge("scope1", BrowserBridgeMetadata{
		ContainerName: "c1",
		ConfigHash:    "old",
		Running:       true,
	})
	if r.ShouldRecreate("scope1", "new") {
		t.Fatal("should not recreate when hot mismatch")
	}
}

func TestConfigHashRegistryShouldRecreateMatched(t *testing.T) {
	r := NewConfigHashRegistry()
	r.RegisterBridge("scope1", BrowserBridgeMetadata{
		ConfigHash: "same",
		Running:    true,
	})
	if r.ShouldRecreate("scope1", "same") {
		t.Fatal("should not recreate when matched")
	}
}

func TestConfigHashRegistryCheckHashMissingBridge(t *testing.T) {
	r := NewConfigHashRegistry()
	matched, isHot, warning := r.CheckHash("nope", "hash")
	if matched || isHot || warning != "" {
		t.Fatalf("matched=%v isHot=%v warning=%q", matched, isHot, warning)
	}
}

func TestConfigHashRegistryUpdateLastUsed(t *testing.T) {
	r := NewConfigHashRegistry()
	r.RegisterBridge("scope1", BrowserBridgeMetadata{ContainerName: "c1"})
	old := r.bridges["scope1"].LastUsedAt
	time.Sleep(time.Millisecond)
	if !r.UpdateLastUsed("scope1") {
		t.Fatal("update should return true")
	}
	if !r.bridges["scope1"].LastUsedAt.After(old) {
		t.Fatal("LastUsedAt should be updated")
	}
}

func TestConfigHashRegistryUpdateLastUsedMissing(t *testing.T) {
	r := NewConfigHashRegistry()
	if r.UpdateLastUsed("nope") {
		t.Fatal("should return false for missing bridge")
	}
}

func TestConfigHashRegistryListBridges(t *testing.T) {
	r := NewConfigHashRegistry()
	r.RegisterBridge("a", BrowserBridgeMetadata{ContainerName: "ca"})
	r.RegisterBridge("b", BrowserBridgeMetadata{ContainerName: "cb"})
	list := r.ListBridges()
	if len(list) != 2 {
		t.Fatalf("count=%d", len(list))
	}
}

func TestConfigHashRegistryBridgeCount(t *testing.T) {
	r := NewConfigHashRegistry()
	r.RegisterBridge("a", BrowserBridgeMetadata{})
	r.RegisterBridge("b", BrowserBridgeMetadata{})
	if r.BridgeCount() != 2 {
		t.Fatalf("count=%d", r.BridgeCount())
	}
}

func TestConfigHashRegistrySetRunning(t *testing.T) {
	r := NewConfigHashRegistry()
	r.RegisterBridge("scope1", BrowserBridgeMetadata{ContainerName: "c1", Running: false})
	if !r.SetRunning("scope1", true) {
		t.Fatal("SetRunning should return true")
	}
	meta, _ := r.GetBridge("scope1")
	if !meta.Running {
		t.Fatal("should be running")
	}
}

func TestIsHotWindow(t *testing.T) {
	if !IsHotWindow(time.Now()) {
		t.Fatal("now should be hot")
	}
	if IsHotWindow(time.Now().Add(-10 * time.Minute)) {
		t.Fatal("10 min ago should be cold")
	}
}

func TestHotBrowserWindow5Min(t *testing.T) {
	if HotBrowserWindow != 5*time.Minute {
		t.Fatalf("window=%v want 5min", HotBrowserWindow)
	}
}

func TestConfigHashRegistryConcurrentAccess(t *testing.T) {
	r := NewConfigHashRegistry()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			r.RegisterBridge("scope1", BrowserBridgeMetadata{ContainerName: "c1"})
		}
	}()
	for i := 0; i < 100; i++ {
		_, _ = r.GetBridge("scope1")
	}
	<-done
}
