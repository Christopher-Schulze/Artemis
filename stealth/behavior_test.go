package stealth

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReferrerDomainMemory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "domain.db")
	mem, err := OpenDomainMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	defer mem.Close()
	if err := mem.Remember(DomainMemoryEntry{
		Domain: "shop.example.com", Purpose: "price_monitor",
		AckID: "ack-1", Level: StealthParanoid,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	ref, err := ReferrerForDomain("https://shop.example.com/item", mem)
	if err != nil {
		t.Fatal(err)
	}
	if ref == "" || !strings.Contains(ref, "google.com") {
		t.Fatalf("expected google referrer, got %q", ref)
	}
	local, err := ReferrerForDomain("http://nas.local/share", mem)
	if err != nil {
		t.Fatal(err)
	}
	if local != "" {
		t.Fatalf("LAN must not inject referrer, got %q", local)
	}
}
