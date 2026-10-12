package network

import (
	"crypto/tls"
	"strings"
	"testing"
)

func TestChrome145FingerprintDefaults(t *testing.T) {
	f := Chrome145Fingerprint()
	if f.Browser != "chrome" {
		t.Errorf("Browser = %q, want \"chrome\"", f.Browser)
	}
	if f.Version != "145" {
		t.Errorf("Version = %q, want \"145\"", f.Version)
	}
	if f.JA3 == "" {
		t.Error("JA3 is empty")
	}
	if f.JA3Hash == "" {
		t.Error("JA3Hash is empty")
	}
	if !strings.HasPrefix(f.JA3, "771,") {
		t.Errorf("JA3 = %q, want TLS 1.3 (771) prefix", f.JA3)
	}
	if len(f.ALPN) != 2 || f.ALPN[0] != "h2" || f.ALPN[1] != "http/1.1" {
		t.Errorf("ALPN = %v, want [h2 http/1.1]", f.ALPN)
	}
	if len(f.CipherSuites) == 0 {
		t.Error("CipherSuites empty")
	}
	if len(f.Curves) != 3 {
		t.Errorf("Curves len = %d, want 3", len(f.Curves))
	}
	if f.Curves[0] != tls.X25519 {
		t.Errorf("Curves[0] = %v, want X25519", f.Curves[0])
	}
	if len(f.SignatureAlgorithms) == 0 {
		t.Error("SignatureAlgorithms empty")
	}
}

func TestTLSFingerprintHasParity(t *testing.T) {
	f := Chrome145Fingerprint()
	if !f.HasParity() {
		t.Error("Chrome145Fingerprint HasParity() = false, want true")
	}
	// Missing JA3 hash -> no parity.
	f.JA3Hash = ""
	if f.HasParity() {
		t.Error("HasParity() with empty JA3Hash = true, want false")
	}
	// Missing HTTP2 -> no parity.
	f = Chrome145Fingerprint()
	f.HTTP2Fingerprint = ""
	if f.HasParity() {
		t.Error("HasParity() with empty HTTP2 = true, want false")
	}
	// Missing HTTP3 -> no parity.
	f = Chrome145Fingerprint()
	f.HTTP3Fingerprint = ""
	if f.HasParity() {
		t.Error("HasParity() with empty HTTP3 = true, want false")
	}
}

func TestTLSFingerprintVerifyJA3Match(t *testing.T) {
	f := Chrome145Fingerprint()
	if !f.VerifyJA3(f.JA3) {
		t.Error("VerifyJA3(JA3) = false, want true")
	}
	if !f.VerifyJA3(f.JA3Hash) {
		t.Error("VerifyJA3(JA3Hash) = false, want true")
	}
}

func TestTLSFingerprintVerifyJA3Mismatch(t *testing.T) {
	f := Chrome145Fingerprint()
	if f.VerifyJA3("771,9999,0,29-23-24,0") {
		t.Error("VerifyJA3(mismatch) = true, want false")
	}
	if f.VerifyJA3("deadbeef") {
		t.Error("VerifyJA3(bad hash) = true, want false")
	}
	if f.VerifyJA3("") {
		t.Error("VerifyJA3(\"\") = true, want false")
	}
}

func TestTLSFingerprintVerifyHTTP2(t *testing.T) {
	f := Chrome145Fingerprint()
	if !f.VerifyHTTP2(f.HTTP2Fingerprint) {
		t.Error("VerifyHTTP2(match) = false, want true")
	}
	if f.VerifyHTTP2("1:4096;2:0") {
		t.Error("VerifyHTTP2(mismatch) = true, want false")
	}
	if f.VerifyHTTP2("") {
		t.Error("VerifyHTTP2(\"\") = true, want false")
	}
	// Empty expected -> always false.
	f2 := f
	f2.HTTP2Fingerprint = ""
	if f2.VerifyHTTP2("anything") {
		t.Error("VerifyHTTP2 with empty expected = true, want false")
	}
}

func TestTLSFingerprintVerifyHTTP3(t *testing.T) {
	f := Chrome145Fingerprint()
	if !f.VerifyHTTP3(f.HTTP3Fingerprint) {
		t.Error("VerifyHTTP3(match) = false, want true")
	}
	if f.VerifyHTTP3("wrong") {
		t.Error("VerifyHTTP3(mismatch) = true, want false")
	}
	if f.VerifyHTTP3("") {
		t.Error("VerifyHTTP3(\"\") = true, want false")
	}
}

func TestTLSFingerprintToTLSConfig(t *testing.T) {
	f := Chrome145Fingerprint()
	cfg := f.ToTLSConfig()
	if cfg == nil {
		t.Fatal("ToTLSConfig() returned nil")
	}
	if len(cfg.NextProtos) != 2 {
		t.Errorf("NextProtos len = %d, want 2", len(cfg.NextProtos))
	}
	if cfg.NextProtos[0] != "h2" || cfg.NextProtos[1] != "http/1.1" {
		t.Errorf("NextProtos = %v, want [h2 http/1.1]", cfg.NextProtos)
	}
	if len(cfg.CipherSuites) != len(f.CipherSuites) {
		t.Errorf("CipherSuites len = %d, want %d", len(cfg.CipherSuites), len(f.CipherSuites))
	}
	for i, c := range cfg.CipherSuites {
		if c != f.CipherSuites[i] {
			t.Errorf("CipherSuites[%d] = %d, want %d", i, c, f.CipherSuites[i])
		}
	}
	if len(cfg.CurvePreferences) != len(f.Curves) {
		t.Errorf("CurvePreferences len = %d, want %d", len(cfg.CurvePreferences), len(f.Curves))
	}
	if cfg.CurvePreferences[0] != tls.X25519 {
		t.Errorf("CurvePreferences[0] = %v, want X25519", cfg.CurvePreferences[0])
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %d, want TLS 1.2", cfg.MinVersion)
	}
	// Mutating returned config must not affect fingerprint.
	cfg.NextProtos[0] = "mutated"
	if f.ALPN[0] == "mutated" {
		t.Error("ToTLSConfig did not copy ALPN; fingerprint mutated")
	}
}

func TestTLSFingerprintMatchScore100(t *testing.T) {
	f := Chrome145Fingerprint()
	score := f.MatchScore(f.JA3, f.HTTP2Fingerprint, f.HTTP3Fingerprint)
	if score != 100 {
		t.Errorf("MatchScore(all match) = %d, want 100", score)
	}
}

func TestTLSFingerprintMatchScore0(t *testing.T) {
	f := Chrome145Fingerprint()
	score := f.MatchScore("wrong", "wrong", "wrong")
	if score != 0 {
		t.Errorf("MatchScore(all mismatch) = %d, want 0", score)
	}
}

func TestTLSFingerprintMatchScorePartial(t *testing.T) {
	f := Chrome145Fingerprint()
	// 1 of 3 matches -> ~33%.
	score := f.MatchScore(f.JA3, "wrong", "wrong")
	if score != 33 {
		t.Errorf("MatchScore(1/3) = %d, want 33", score)
	}
	// 2 of 3 matches -> ~66%.
	score = f.MatchScore(f.JA3, f.HTTP2Fingerprint, "wrong")
	if score != 66 {
		t.Errorf("MatchScore(2/3) = %d, want 66", score)
	}
}

func TestTLSFingerprintString(t *testing.T) {
	f := Chrome145Fingerprint()
	s := f.String()
	if !strings.Contains(s, "chrome/145") {
		t.Errorf("String() = %q, want browser/version", s)
	}
	if !strings.Contains(s, f.JA3Hash) {
		t.Errorf("String() = %q, missing JA3 hash", s)
	}
}
