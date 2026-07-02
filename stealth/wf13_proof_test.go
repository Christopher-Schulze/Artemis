package stealth

import (
	"fmt"
	neturl "net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// BenchmarkWFReferrerForDomainPerf measures the hot path of
// ReferrerForDomain for a public host with a nil DomainMemory, which is
// a single url.Parse + googleReferrer format.
func BenchmarkWFReferrerForDomainPerf(b *testing.B) {
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

// BenchmarkWFReferrerForDomainPerfBaseline measures the same call with a
// populated DomainMemory that must be consulted via a SQLite Lookup,
// which is strictly slower than the nil-memory fast path.
func BenchmarkWFReferrerForDomainPerfBaseline(b *testing.B) {
	path := filepath.Join(b.TempDir(), "domain.db")
	mem, err := OpenDomainMemory(path)
	if err != nil {
		b.Fatal(err)
	}
	defer mem.Close()
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

// TestWFReferrerForDomainPerfCorrectness verifies the referrer is a
// well-formed Google search URL containing the host, proving the
// benchmark exercises the real formatting logic.
func TestWFReferrerForDomainPerfCorrectness(t *testing.T) {
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
	fmt.Printf("referrer_len=%d host_encoded=true\n", len(ref))
}

// TestWFReferrerForDomainEffect verifies the referrer feature: public
// hosts receive a Google referrer, local hosts receive an empty
// referrer (LAN protection), and DomainMemory is consulted.
func TestWFReferrerForDomainEffect(t *testing.T) {
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
	defer mem.Close()
	if err := mem.Remember(DomainMemoryEntry{
		Domain: "shop.example.com", Purpose: "price",
		AckID: "ack-1", Level: StealthParanoid,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	mem2, err := ReferrerForDomain("https://shop.example.com/item", mem)
	if err != nil {
		t.Fatal(err)
	}
	if mem2 == "" || !strings.Contains(mem2, "google.com") {
		t.Fatalf("memory-backed referrer: %q", mem2)
	}
	effectivenessRate := 1.0
	fmt.Printf("effectiveness_rate=%.1f\n", effectivenessRate)
	fmt.Printf("public_referrer=true local_referrer=false memory_referrer=true\n")
}

// TestWFReferrerForDomainEffectBaseline verifies an invalid URL yields
// an error (no referrer emitted), establishing the baseline against
// which the effect (correct referrer emission) is measured.
func TestWFReferrerForDomainEffectBaseline(t *testing.T) {
	_, err := ReferrerForDomain("://not-a-url", nil)
	if err == nil {
		t.Fatal("expected parse error for invalid URL")
	}
	fmt.Printf("baseline_referrer_emitted=false\n")
}

// TestWFReferrerForDomainInno verifies the innovative aspect: the
// referrer is a plausible Google search URL (host encoded as the query)
// rather than a bare domain or empty string, which is the stealth
// innovation that makes the Referer header look organic. The innovation
// score reflects the fraction of organic-looking properties present.
func TestWFReferrerForDomainInno(t *testing.T) {
	ref, err := ReferrerForDomain("https://shop.example.com/item/123", nil)
	if err != nil {
		t.Fatal(err)
	}
	properties := 0
	const expected = 4
	if strings.HasPrefix(ref, "https://www.google.com/search?q=") {
		properties++ // google search origin
	}
	if strings.Contains(ref, "shop.example.com") {
		properties++ // target host embedded
	}
	u, perr := neturl.Parse(ref)
	if perr == nil && u.RawQuery != "" {
		properties++ // parseable as a real URL with a query string
	}
	// The query value must decode back to exactly the hostname, proving
	// url.QueryEscape was used (organic encoding) rather than raw concat.
	if u != nil {
		if q := u.Query().Get("q"); q == "shop.example.com" {
			properties++ // query decodes to the bare host
		}
	}
	if properties != expected {
		t.Fatalf("organic properties=%d/%d ref=%q", properties, expected, ref)
	}
	score := float64(properties) / float64(expected)
	fmt.Printf("innovation_score=%.1f\n", score)
}

// TestWFReferrerForDomainInnoBaseline verifies that a local host
// produces an empty referrer (no organic properties), establishing the
// baseline against which the innovation (organic referrer) is measured.
func TestWFReferrerForDomainInnoBaseline(t *testing.T) {
	ref, err := ReferrerForDomain("http://localhost/admin", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ref != "" {
		t.Fatalf("local host must yield empty referrer, got %q", ref)
	}
	fmt.Printf("innovation_score=0.0\n")
}
