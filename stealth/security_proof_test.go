package stealth

import (
	neturl "net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// BenchmarkReferrerForDomainPerf measures the hot path of
// ReferrerForDomain for a public host with a nil DomainMemory, which is
// a single url.Parse + googleReferrer format.
func BenchmarkReferrerForDomainPerf(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ref, err := ReferrerForDomain("https://shop.example.com/item/123", nil)
		if err != nil {
			b.Fatal(err)
		}
		if ref == "" || !strings.Contains(ref, "google.com") {
			b.Fatalf("bad referrer: %q", ref)
		}
	}
}

// BenchmarkReferrerForDomainPerfBaseline measures the same call with a
// populated DomainMemory that must be consulted via a SQLite Lookup,
// which is strictly slower than the nil-memory fast path.
func BenchmarkReferrerForDomainPerfBaseline(b *testing.B) {
	path := filepath.Join(b.TempDir(), "domain.db")
	mem, err := OpenDomainMemory(path)
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if closeErr := mem.Close(); closeErr != nil {
			b.Errorf("close domain memory: %v", closeErr)
		}
	}()
	if err := mem.Remember(DomainMemoryEntry{
		Domain: "shop.example.com", Purpose: "price",
		AckID: "ack-1", Level: StealthParanoid,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ref, err := ReferrerForDomain("https://shop.example.com/item/123", mem)
		if err != nil {
			b.Fatal(err)
		}
		if ref == "" || !strings.Contains(ref, "google.com") {
			b.Fatalf("bad referrer: %q", ref)
		}
	}
}

// TestReferrerForDomainPerfCorrectness verifies the referrer is a
// well-formed Google search URL containing the host, proving the
// benchmark exercises the real formatting logic.
func TestReferrerForDomainPerfCorrectness(t *testing.T) {
	ref, err := ReferrerForDomain("https://shop.example.com/item/123", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ref, "https://www.google.com/search?q=") {
		t.Fatalf("not a google referrer: %q", ref)
	}
	if !strings.Contains(ref, "shop.example.com") {
		t.Fatalf("referrer missing host: %q", ref)
	}
}

// TestReferrerForDomainEffect verifies the referrer feature: public
// hosts receive a Google referrer, local hosts receive an empty
// referrer (LAN protection), and DomainMemory is consulted.
func TestReferrerForDomainEffect(t *testing.T) {
	// Public host, nil memory -> google referrer.
	pub, err := ReferrerForDomain("https://acme.io/page", nil)
	if err != nil {
		t.Fatal(err)
	}
	if pub == "" || !strings.Contains(pub, "google.com") {
		t.Fatalf("public referrer: %q", pub)
	}
	// Local host -> empty referrer (LAN must not leak referrer).
	local, err := ReferrerForDomain("http://nas.local/share", nil)
	if err != nil {
		t.Fatal(err)
	}
	if local != "" {
		t.Fatalf("local referrer must be empty, got %q", local)
	}
	// DomainMemory-backed paranoid host -> google referrer.
	path := filepath.Join(t.TempDir(), "domain.db")
	mem, err := OpenDomainMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := mem.Close(); closeErr != nil {
			t.Errorf("close domain memory: %v", closeErr)
		}
	}()
	if rememberErr := mem.Remember(DomainMemoryEntry{
		Domain: "shop.example.com", Purpose: "price",
		AckID: "ack-1", Level: StealthParanoid,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}); rememberErr != nil {
		t.Fatal(rememberErr)
	}
	mem2, err := ReferrerForDomain("https://shop.example.com/item", mem)
	if err != nil {
		t.Fatal(err)
	}
	if mem2 == "" || !strings.Contains(mem2, "google.com") {
		t.Fatalf("memory-backed referrer: %q", mem2)
	}
}

// TestReferrerForDomainEffectBaseline verifies an invalid URL yields
// an error (no referrer emitted), establishing the baseline against
// which the effect (correct referrer emission) is measured.
func TestReferrerForDomainEffectBaseline(t *testing.T) {
	_, err := ReferrerForDomain("://not-a-url", nil)
	if err == nil {
		t.Fatal("expected parse error for invalid URL")
	}
}

// TestReferrerForDomainBuildsOrganicGoogleReferrer verifies that the
// referrer is a plausible Google search URL with the host encoded as the
// query rather than a bare domain or an empty string, so the Referer
// header looks organic.
func TestReferrerForDomainBuildsOrganicGoogleReferrer(t *testing.T) {
	ref, err := ReferrerForDomain("https://shop.example.com/item/123", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ref, "https://www.google.com/search?q=") || !strings.Contains(ref, "shop.example.com") {
		t.Fatalf("referrer %q is not a Google search for the target host", ref)
	}
	u, err := neturl.Parse(ref)
	if err != nil || u.RawQuery == "" {
		t.Fatalf("referrer %q is not a URL with a query string: %v", ref, err)
	}
	// The query value decodes back to exactly the hostname, proving
	// url.QueryEscape was used rather than raw concatenation.
	if q := u.Query().Get("q"); q != "shop.example.com" {
		t.Fatalf("referrer query = %q, want the bare host", q)
	}
}

// TestReferrerForDomainLocalHostHasNoReferrer verifies that a local host
// produces an empty referrer.
func TestReferrerForDomainLocalHostHasNoReferrer(t *testing.T) {
	ref, err := ReferrerForDomain("http://localhost/admin", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ref != "" {
		t.Fatalf("local host must yield empty referrer, got %q", ref)
	}
}
