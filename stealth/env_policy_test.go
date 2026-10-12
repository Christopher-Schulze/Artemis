package stealth

import (
	"testing"
)

func TestPolicyFromEnvUnset(t *testing.T) {
	t.Setenv(EnvStealthLevel, "")
	policy, requested, err := PolicyFromEnv()
	if err != nil || requested {
		t.Fatalf("requested=%v err=%v", requested, err)
	}
	if policy.PublicDefault != StealthDefault {
		t.Fatalf("level = %q, want default", policy.PublicDefault)
	}
}

func TestPolicyFromEnvStealthRequiresAck(t *testing.T) {
	t.Setenv(EnvStealthLevel, "stealth")
	t.Setenv(EnvStealthPurpose, "")
	t.Setenv(EnvStealthLegalBasis, "")
	if _, requested, err := PolicyFromEnv(); !requested || err == nil {
		t.Fatalf("requested=%v err=%v, want hard error for missing ack", requested, err)
	}
}

func TestPolicyFromEnvStealthComplete(t *testing.T) {
	t.Setenv(EnvStealthLevel, "paranoid")
	t.Setenv(EnvStealthPurpose, "monitoring own properties")
	t.Setenv(EnvStealthLegalBasis, "legitimate interest")
	t.Setenv(EnvStealthDomains, "example.com, *.example.org")
	policy, requested, err := PolicyFromEnv()
	if err != nil || !requested {
		t.Fatalf("requested=%v err=%v", requested, err)
	}
	if policy.PublicDefault != StealthParanoid {
		t.Fatalf("level = %q", policy.PublicDefault)
	}
	if policy.Ack.LegalBasis == "" || policy.Ack.Purpose == "" || policy.Ack.AcknowledgedAt.IsZero() {
		t.Fatal("ack fields incomplete")
	}
	if len(policy.Ack.DomainAllow) != 2 {
		t.Fatalf("domains = %v", policy.Ack.DomainAllow)
	}
	// The ack must actually pass the level gate for an allowed host.
	level, err := DetermineStealthLevel("https://example.com/", policy, nil)
	if err != nil || level != StealthParanoid {
		t.Fatalf("level=%q err=%v", level, err)
	}
	// A non-allowlisted host must NOT get stealth.
	level, err = DetermineStealthLevel("https://other.example.net/", policy, nil)
	if err != nil || level != StealthDefault {
		t.Fatalf("non-allowlisted host level=%q err=%v", level, err)
	}
}

func TestPolicyFromEnvRejectsUnknownLevel(t *testing.T) {
	t.Setenv(EnvStealthLevel, "ninja")
	if _, requested, err := PolicyFromEnv(); !requested || err == nil {
		t.Fatalf("requested=%v err=%v", requested, err)
	}
}
