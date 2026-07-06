package stealth

import (
	"net"
	"testing"
	"time"
)

func TestStealthPrivateIPGate(t *testing.T) {
	lookup := func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("192.168.1.10")}, nil
	}
	level, err := DetermineStealthLevel("http://app.internal/dashboard", StealthPolicy{
		PublicDefault: StealthParanoid,
		Requested:     StealthParanoid,
		Ack: StealthAck{
			AcknowledgedAt: time.Now(),
			LegalBasis:     "contract",
			Purpose:        "automation",
			ExpiresAt:      time.Now().Add(24 * time.Hour),
		},
	}, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if level != StealthDefault {
		t.Fatalf("private IP must force default, got %s", level)
	}
}

func TestDetermineStealthLevel(t *testing.T) {
	lookup := func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	ack := StealthAck{
		AcknowledgedAt: time.Now(),
		LegalBasis:     "legitimate_interest",
		Purpose:        "scraping",
		DomainAllow:    []string{"example.com"},
		ExpiresAt:      time.Now().Add(365 * 24 * time.Hour),
	}
	level, err := DetermineStealthLevel("https://example.com/page", StealthPolicy{
		PublicDefault: StealthDefault,
		Requested:     StealthStealth,
		Ack:           ack,
	}, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if level != StealthStealth {
		t.Fatalf("expected stealth with valid ack, got %s", level)
	}
	if PatchCountFor(level) != 27 {
		t.Fatalf("expected 27 patches, got %d", PatchCountFor(level))
	}
	level2, _ := DetermineStealthLevel("https://blocked.example/page", StealthPolicy{
		Requested: StealthParanoid,
		Ack:       ack,
	}, lookup)
	if level2 != StealthDefault {
		t.Fatalf("domain not allowlisted must not escalate, got %s", level2)
	}
}
