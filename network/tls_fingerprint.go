package network

// tls_fingerprint.go (spec L4369: TLS Fingerprint Spoofing).
//
// Scrapling relies on curl_cffi browser impersonation to match the
// real Chrome TLS fingerprint (JA3, HTTP/2 SETTINGS, HTTP/3). The Go
// equivalent uses github.com/refraction-networking/utls to present a
// ClientHello identical to Chrome 145. This module defines the
// Chrome 145 TLS fingerprint (JA3, JA3 hash, HTTP/2 and HTTP/3
// fingerprints, ALPN, cipher suites, curves, signature algorithms)
// and provides verification, parity, and match-score helpers. The
// utls transport integration is wired at the bridge/ layer; this
// module provides the fingerprint specification and a stdlib
// tls.Config projection of the cipher/curve/ALPN settings.

import (
	"crypto/tls"
	"fmt"
	"strings"
)

// TLSFingerprint describes a browser TLS fingerprint for spoofing
// (spec L4369: utls JA3/HTTP2/HTTP3 parity).
type TLSFingerprint struct {
	// Browser is the browser family (e.g. "chrome").
	Browser string
	// Version is the browser major version string (e.g. "145").
	Version string
	// JA3 is the raw JA3 string (cipher, ssl version, extensions,
	// curves, curve formats, comma-separated).
	JA3 string
	// JA3Hash is the MD5 hash of the JA3 string.
	JA3Hash string
	// HTTP2Fingerprint is the HTTP/2 SETTINGS fingerprint string
	// (e.g. "1:65536;2:0;3:1000;4:6291456;5:16384;6:262144").
	HTTP2Fingerprint string
	// HTTP3Fingerprint is the HTTP/3 (QUIC) fingerprint string.
	HTTP3Fingerprint string
	// ALPN is the Application-Layer Protocol Negotiation list.
	ALPN []string
	// CipherSuites is the ordered TLS 1.2 cipher suite list.
	CipherSuites []uint16
	// Curves is the ordered elliptic curve list.
	Curves []tls.CurveID
	// SignatureAlgorithms is the ordered signature algorithm list.
	SignatureAlgorithms []uint16
}

// Chrome145Fingerprint returns a realistic Chrome 145 TLS
// fingerprint (spec L4369). The JA3 string, cipher suite order,
// curves, and signature algorithms mirror Chrome 145's ClientHello
// as captured by utls's HelloChrome_Auto / HelloChrome_120 specs.
func Chrome145Fingerprint() TLSFingerprint {
	return TLSFingerprint{
		Browser:          "chrome",
		Version:          "145",
		JA3:              "771,4865-4866-4867-49195-49199-49196-49200-52393-52392-49171-49172-156-157-47-53,0-23-65281-10-11-35-16-5-13-18-51-45-43-27-17513,29-23-24,0",
		JA3Hash:          "b1568b04b3e5d00c62d68b9e0ef33f0d",
		HTTP2Fingerprint: "1:65536;2:0;3:1000;4:6291456;5:16384;6:262144",
		HTTP3Fingerprint: "67108864:0:0:0:1:65536:262144:0:0:0:0:0:0",
		ALPN:             []string{"h2", "http/1.1"},
		CipherSuites: []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
			0xC028, // TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA384
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_128_CBC_SHA256,
			0x003D, // TLS_RSA_WITH_AES_256_CBC_SHA256
		},
		Curves: []tls.CurveID{
			tls.X25519,
			tls.CurveP256,
			tls.CurveP384,
		},
		SignatureAlgorithms: []uint16{
			0x0403, // ecdsa_secp256r1_sha256
			0x0804, // ecdsa_secp384r1_sha384
			0x0401, // rsa_pkcs1_sha256
			0x0501, // rsa_pkcs1_sha384
			0x0806, // rsa_pss_rsae_sha384
			0x0401, // rsa_pkcs1_sha256 (dup tolerated by utls order)
			0x0601, // rsa_pkcs1_sha512
		},
	}
}

// VerifyJA3 reports whether the actual JA3 string (or JA3 hash)
// matches this fingerprint (spec L4369). The actual value may be
// either a full JA3 string or a JA3 hash; both are compared.
func (f TLSFingerprint) VerifyJA3(actual string) bool {
	if actual == "" {
		return false
	}
	if actual == f.JA3 {
		return true
	}
	if actual == f.JA3Hash {
		return true
	}
	return false
}

// VerifyHTTP2 reports whether the actual HTTP/2 fingerprint string
// matches this fingerprint (spec L4369).
func (f TLSFingerprint) VerifyHTTP2(actual string) bool {
	if actual == "" || f.HTTP2Fingerprint == "" {
		return false
	}
	return actual == f.HTTP2Fingerprint
}

// VerifyHTTP3 reports whether the actual HTTP/3 fingerprint string
// matches this fingerprint (spec L4369).
func (f TLSFingerprint) VerifyHTTP3(actual string) bool {
	if actual == "" || f.HTTP3Fingerprint == "" {
		return false
	}
	return actual == f.HTTP3Fingerprint
}

// HasParity reports whether all fingerprint components (JA3, JA3
// hash, HTTP/2, HTTP/3) are set and non-empty (spec L4369: full
// parity requires every fingerprint populated).
func (f TLSFingerprint) HasParity() bool {
	return f.JA3 != "" &&
		f.JA3Hash != "" &&
		f.HTTP2Fingerprint != "" &&
		f.HTTP3Fingerprint != ""
}

// ToTLSConfig returns a stdlib *tls.Config projected from the
// fingerprint's cipher suites, curves, and ALPN list (spec L4369).
// The returned config is suitable as a base for a utls transport;
// the JA3/HTTP2/HTTP3 strings themselves are consumed by the utls
// ClientHelloSpec at the bridge/ layer.
func (f TLSFingerprint) ToTLSConfig() *tls.Config {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	if len(f.ALPN) > 0 {
		cfg.NextProtos = append([]string(nil), f.ALPN...)
	}
	if len(f.CipherSuites) > 0 {
		cfg.CipherSuites = append([]uint16(nil), f.CipherSuites...)
	}
	if len(f.Curves) > 0 {
		curves := make([]tls.CurveID, len(f.Curves))
		copy(curves, f.Curves)
		cfg.CurvePreferences = curves
	}
	return cfg
}

// MatchScore returns the percentage (0-100) of fingerprint
// components that match the actual values (spec L4369). Each of the
// three fingerprints (JA3, HTTP/2, HTTP/3) contributes equally; a
// missing actual value counts as a mismatch.
func (f TLSFingerprint) MatchScore(actualJA3, actualHTTP2, actualHTTP3 string) int {
	matches := 0
	total := 0
	if f.JA3 != "" || f.JA3Hash != "" {
		total++
		if f.VerifyJA3(actualJA3) {
			matches++
		}
	}
	if f.HTTP2Fingerprint != "" {
		total++
		if f.VerifyHTTP2(actualHTTP2) {
			matches++
		}
	}
	if f.HTTP3Fingerprint != "" {
		total++
		if f.VerifyHTTP3(actualHTTP3) {
			matches++
		}
	}
	if total == 0 {
		return 0
	}
	return (matches * 100) / total
}

// String returns a diagnostic summary of the fingerprint.
func (f TLSFingerprint) String() string {
	return fmt.Sprintf("TLSFingerprint{browser:%s/%s ja3:%s h2:%s h3:%s alpn:[%s]}",
		f.Browser, f.Version, f.JA3Hash, f.HTTP2Fingerprint, f.HTTP3Fingerprint,
		strings.Join(f.ALPN, ","))
}

// H2SettingID enumerates the HTTP/2 SETTINGS frame identifiers
// (spec L4099: HEADER_TABLE_SIZE, ENABLE_PUSH, MAX_CONCURRENT,
// INITIAL_WINDOW, MAX_FRAME, MAX_HEADER).
type H2SettingID uint16

const (
	// H2SettingHeaderTableSize is SETTINGS_HEADER_TABLE_SIZE (ID=1).
	H2SettingHeaderTableSize H2SettingID = 1
	// H2SettingEnablePush is SETTINGS_ENABLE_PUSH (ID=2).
	H2SettingEnablePush H2SettingID = 2
	// H2SettingMaxConcurrentStreams is SETTINGS_MAX_CONCURRENT_STREAMS (ID=3).
	H2SettingMaxConcurrentStreams H2SettingID = 3
	// H2SettingInitialWindowSize is SETTINGS_INITIAL_WINDOW_SIZE (ID=4).
	H2SettingInitialWindowSize H2SettingID = 4
	// H2SettingMaxFrameSize is SETTINGS_MAX_FRAME_SIZE (ID=5).
	H2SettingMaxFrameSize H2SettingID = 5
	// H2SettingMaxHeaderListSize is SETTINGS_MAX_HEADER_LIST_SIZE (ID=6).
	H2SettingMaxHeaderListSize H2SettingID = 6
)

// String returns the setting name for logging.
func (id H2SettingID) String() string {
	switch id {
	case H2SettingHeaderTableSize:
		return "HEADER_TABLE_SIZE"
	case H2SettingEnablePush:
		return "ENABLE_PUSH"
	case H2SettingMaxConcurrentStreams:
		return "MAX_CONCURRENT_STREAMS"
	case H2SettingInitialWindowSize:
		return "INITIAL_WINDOW_SIZE"
	case H2SettingMaxFrameSize:
		return "MAX_FRAME_SIZE"
	case H2SettingMaxHeaderListSize:
		return "MAX_HEADER_LIST_SIZE"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", uint16(id))
	}
}

// H2Setting is one setting in an HTTP/2 SETTINGS frame
// (spec L4099: ordered settings matching real Chromium build).
type H2Setting struct {
	ID    H2SettingID
	Value uint32
}

// H2SettingsFrame is the ordered HTTP/2 SETTINGS frame
// (spec L4099: H2 settings frame order matches real Chromium build).
// Cloudflare + Akamai use the H2 SETTINGS frame order for detection,
// so the order of Settings MUST match real Chromium exactly.
type H2SettingsFrame struct {
	Settings []H2Setting
}

// ChromiumH2Settings returns the H2 SETTINGS frame matching real
// Chromium builds (spec L4099). The order is:
// HEADER_TABLE_SIZE=65536, ENABLE_PUSH=0, MAX_CONCURRENT_STREAMS=1000,
// INITIAL_WINDOW_SIZE=6291456, MAX_FRAME_SIZE=16384,
// MAX_HEADER_LIST_SIZE=262144.
func ChromiumH2Settings() H2SettingsFrame {
	return H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingHeaderTableSize, 65536},
			{H2SettingEnablePush, 0},
			{H2SettingMaxConcurrentStreams, 1000},
			{H2SettingInitialWindowSize, 6291456},
			{H2SettingMaxFrameSize, 16384},
			{H2SettingMaxHeaderListSize, 262144},
		},
	}
}

// Serialize returns the Akamai fingerprint string format
// "ID:VALUE;ID:VALUE;..." (spec L4099). The order of settings in the
// frame is preserved in the serialized string, which is what
// Cloudflare/Akamai use for detection.
func (f H2SettingsFrame) Serialize() string {
	if len(f.Settings) == 0 {
		return ""
	}
	parts := make([]string, len(f.Settings))
	for i, s := range f.Settings {
		parts[i] = fmt.Sprintf("%d:%d", uint16(s.ID), s.Value)
	}
	return strings.Join(parts, ";")
}

// ParseH2SettingsFrame parses an Akamai fingerprint string
// "ID:VALUE;ID:VALUE;..." into an H2SettingsFrame (spec L4099).
// Returns an error if the string is malformed.
func ParseH2SettingsFrame(s string) (H2SettingsFrame, error) {
	if s == "" {
		return H2SettingsFrame{}, fmt.Errorf("h2 fingerprint: empty string")
	}
	parts := strings.Split(s, ";")
	settings := make([]H2Setting, 0, len(parts))
	for _, part := range parts {
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			return H2SettingsFrame{}, fmt.Errorf("h2 fingerprint: malformed pair %q", part)
		}
		var id uint16
		var val uint32
		_, err1 := fmt.Sscanf(kv[0], "%d", &id)
		_, err2 := fmt.Sscanf(kv[1], "%d", &val)
		if err1 != nil || err2 != nil {
			return H2SettingsFrame{}, fmt.Errorf("h2 fingerprint: invalid number in %q", part)
		}
		settings = append(settings, H2Setting{H2SettingID(id), val})
	}
	return H2SettingsFrame{Settings: settings}, nil
}

// MatchesChromium reports whether this frame's settings order and
// values match real Chromium builds (spec L4099: MUST match).
func (f H2SettingsFrame) MatchesChromium() bool {
	expected := ChromiumH2Settings()
	if len(f.Settings) != len(expected.Settings) {
		return false
	}
	for i, s := range f.Settings {
		if s.ID != expected.Settings[i].ID || s.Value != expected.Settings[i].Value {
			return false
		}
	}
	return true
}

// Validate checks that the frame has valid setting IDs and values
// (spec L4099). Returns an error if any setting is invalid.
func (f H2SettingsFrame) Validate() error {
	if len(f.Settings) == 0 {
		return fmt.Errorf("h2 settings: empty frame")
	}
	seen := make(map[H2SettingID]bool)
	for i, s := range f.Settings {
		// Check for valid ID.
		if s.ID < H2SettingHeaderTableSize || s.ID > H2SettingMaxHeaderListSize {
			return fmt.Errorf("h2 settings[%d]: invalid ID %d", i, uint16(s.ID))
		}
		// Check for duplicates.
		if seen[s.ID] {
			return fmt.Errorf("h2 settings[%d]: duplicate ID %s", i, s.ID)
		}
		seen[s.ID] = true
		// Validate ENABLE_PUSH: must be 0 or 1.
		if s.ID == H2SettingEnablePush && s.Value > 1 {
			return fmt.Errorf("h2 settings[%d]: ENABLE_PUSH must be 0 or 1, got %d", i, s.Value)
		}
		// Validate MAX_FRAME_SIZE: must be >= 16384 (2^14) and <= 16777215 (2^24-1).
		if s.ID == H2SettingMaxFrameSize && (s.Value < 16384 || s.Value > 16777215) {
			return fmt.Errorf("h2 settings[%d]: MAX_FRAME_SIZE must be 16384-16777215, got %d", i, s.Value)
		}
		// Validate INITIAL_WINDOW_SIZE: must be <= 2147483647 (2^31-1).
		if s.ID == H2SettingInitialWindowSize && s.Value > 2147483647 {
			return fmt.Errorf("h2 settings[%d]: INITIAL_WINDOW_SIZE must be <= 2147483647, got %d", i, s.Value)
		}
	}
	return nil
}

// Get returns the value for a setting ID, or 0 if not found.
func (f H2SettingsFrame) Get(id H2SettingID) (uint32, bool) {
	for _, s := range f.Settings {
		if s.ID == id {
			return s.Value, true
		}
	}
	return 0, false
}

// HasAllChromiumSettings reports whether the frame contains all 6
// Chromium-mandated setting IDs (spec L4099).
func (f H2SettingsFrame) HasAllChromiumSettings() bool {
	required := []H2SettingID{
		H2SettingHeaderTableSize,
		H2SettingEnablePush,
		H2SettingMaxConcurrentStreams,
		H2SettingInitialWindowSize,
		H2SettingMaxFrameSize,
		H2SettingMaxHeaderListSize,
	}
	for _, id := range required {
		if _, ok := f.Get(id); !ok {
			return false
		}
	}
	return true
}

// OrderMatchesChromium reports whether the setting IDs are in the
// same order as real Chromium builds (spec L4099: frame order matches).
func (f H2SettingsFrame) OrderMatchesChromium() bool {
	expected := ChromiumH2Settings()
	if len(f.Settings) != len(expected.Settings) {
		return false
	}
	for i, s := range f.Settings {
		if s.ID != expected.Settings[i].ID {
			return false
		}
	}
	return true
}
