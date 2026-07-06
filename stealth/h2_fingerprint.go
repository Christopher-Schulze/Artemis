package stealth

import (
	"fmt"
	"strings"
)

// h2_fingerprint.go (spec L4099: HTTP/2 Fingerprint Parity).
//
// Cloudflare + Akamai use H2 SETTINGS frame order for detection. Real
// browsers send HEADER_TABLE_SIZE, ENABLE_PUSH, MAX_CONCURRENT_STREAMS,
// INITIAL_WINDOW_SIZE, MAX_FRAME_SIZE, MAX_HEADER_LIST_SIZE in a specific
// order. Go's default HTTP/2 client does not match this.
//
// The spec requires github.com/refraction-networking/utls for H2
// fingerprint spoofing. This module defines the Chromium-matching H2
// SETTINGS frame order and provides a validator. The actual utls
// transport integration is wired at the bridge/ layer when utls is
// available; this module provides the frame ordering specification
// that the transport layer consumes.

// H2SettingID is an HTTP/2 SETTINGS frame parameter ID
// (RFC 7540 Section 6.5.2).
type H2SettingID uint16

const (
	// H2SettingHeaderTableSize is SETTINGS_HEADER_TABLE_SIZE (0x1).
	H2SettingHeaderTableSize H2SettingID = 0x1
	// H2SettingEnablePush is SETTINGS_ENABLE_PUSH (0x2).
	H2SettingEnablePush H2SettingID = 0x2
	// H2SettingMaxConcurrentStreams is SETTINGS_MAX_CONCURRENT_STREAMS (0x3).
	H2SettingMaxConcurrentStreams H2SettingID = 0x3
	// H2SettingInitialWindowSize is SETTINGS_INITIAL_WINDOW_SIZE (0x4).
	H2SettingInitialWindowSize H2SettingID = 0x4
	// H2SettingMaxFrameSize is SETTINGS_MAX_FRAME_SIZE (0x5).
	H2SettingMaxFrameSize H2SettingID = 0x5
	// H2SettingMaxHeaderListSize is SETTINGS_MAX_HEADER_LIST_SIZE (0x6).
	H2SettingMaxHeaderListSize H2SettingID = 0x6
	// H2SettingEnableConnectProtocol is SETTINGS_ENABLE_CONNECT_PROTOCOL (0x8).
	H2SettingEnableConnectProtocol H2SettingID = 0x8
)

// H2Setting is a single HTTP/2 SETTINGS frame parameter
// (spec L4099: HEADER_TABLE_SIZE, ENABLE_PUSH, MAX_CONCURRENT,
// INITIAL_WINDOW, MAX_FRAME, MAX_HEADER).
type H2Setting struct {
	ID    H2SettingID `json:"id"`
	Value uint32      `json:"value"`
}

// String returns a human-readable name for the setting ID.
func (s H2Setting) String() string {
	switch s.ID {
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
	case H2SettingEnableConnectProtocol:
		return "ENABLE_CONNECT_PROTOCOL"
	default:
		return fmt.Sprintf("UNKNOWN(0x%x)", uint16(s.ID))
	}
}

// H2SettingsFrame is an ordered sequence of H2 SETTINGS parameters
// (spec L4099: frame order matters for fingerprint parity).
type H2SettingsFrame struct {
	Settings []H2Setting `json:"settings"`
}

// IDs returns the ordered list of setting IDs in this frame.
func (f H2SettingsFrame) IDs() []H2SettingID {
	ids := make([]H2SettingID, len(f.Settings))
	for i, s := range f.Settings {
		ids[i] = s.ID
	}
	return ids
}

// Names returns the ordered list of setting names for diagnostics.
func (f H2SettingsFrame) Names() []string {
	names := make([]string, len(f.Settings))
	for i, s := range f.Settings {
		names[i] = s.String()
	}
	return names
}

// ChromiumH2Settings returns the H2 SETTINGS frame order matching
// real Chromium builds (spec L4099: HEADER_TABLE_SIZE, ENABLE_PUSH,
// MAX_CONCURRENT_STREAMS, INITIAL_WINDOW_SIZE, MAX_FRAME_SIZE,
// MAX_HEADER_LIST_SIZE).
//
// These values are derived from Chromium's net/http2 source and
// represent the default H2 settings that Chrome sends on connection
// establishment. The order is critical because Cloudflare/Akamai
// fingerprint the SETTINGS frame order to detect non-browser clients.
func ChromiumH2Settings() H2SettingsFrame {
	return H2SettingsFrame{
		Settings: []H2Setting{
			{ID: H2SettingHeaderTableSize, Value: 65536},
			{ID: H2SettingEnablePush, Value: 0},
			{ID: H2SettingMaxConcurrentStreams, Value: 1000},
			{ID: H2SettingInitialWindowSize, Value: 6291456},
			{ID: H2SettingMaxFrameSize, Value: 16384},
			{ID: H2SettingMaxHeaderListSize, Value: 262144},
		},
	}
}

// ChromiumH2SettingsOrder returns the expected setting ID order
// for Chromium builds (spec L4099).
func ChromiumH2SettingsOrder() []H2SettingID {
	return []H2SettingID{
		H2SettingHeaderTableSize,
		H2SettingEnablePush,
		H2SettingMaxConcurrentStreams,
		H2SettingInitialWindowSize,
		H2SettingMaxFrameSize,
		H2SettingMaxHeaderListSize,
	}
}

// ValidateH2Settings checks if a settings frame matches the Chromium
// H2 SETTINGS frame order (spec L4099: MUST match real Chromium build
// H2 settings frame order).
//
// Returns an error describing the first mismatch, or nil if the order
// matches.
func ValidateH2Settings(frame H2SettingsFrame) error {
	expected := ChromiumH2SettingsOrder()
	if len(frame.Settings) < len(expected) {
		return fmt.Errorf("h2 fingerprint: frame has %d settings, expected at least %d (Chromium order)", len(frame.Settings), len(expected))
	}

	for i, expectedID := range expected {
		if frame.Settings[i].ID != expectedID {
			return fmt.Errorf("h2 fingerprint: setting at position %d is %s, expected %s (Chromium order)", i, frame.Settings[i].String(), H2Setting{ID: expectedID}.String())
		}
	}

	return nil
}

// ValidateH2SettingsStrict checks if a settings frame exactly matches
// the Chromium H2 SETTINGS frame (both order and values).
func ValidateH2SettingsStrict(frame H2SettingsFrame) error {
	if err := ValidateH2Settings(frame); err != nil {
		return err
	}

	expected := ChromiumH2Settings()
	if len(frame.Settings) != len(expected.Settings) {
		return fmt.Errorf("h2 fingerprint: frame has %d settings, expected exactly %d", len(frame.Settings), len(expected.Settings))
	}

	for i, exp := range expected.Settings {
		if frame.Settings[i].Value != exp.Value {
			return fmt.Errorf("h2 fingerprint: %s value is %d, expected %d", frame.Settings[i].String(), frame.Settings[i].Value, exp.Value)
		}
	}

	return nil
}

// H2FingerprintConfig configures the H2 fingerprint for a browser
// session (spec L4099: utls H2 fingerprint spoofing).
type H2FingerprintConfig struct {
	// Settings is the ordered H2 SETTINGS frame to send.
	Settings H2SettingsFrame `json:"settings"`
	// WindowUpdate is the WINDOW_UPDATE value for the connection-level
	// flow control window (Chromium sends 15663105).
	WindowUpdate uint32 `json:"window_update"`
	// HeaderOrder defines the pseudo-header order for requests.
	// Chromium sends: :method, :authority, :scheme, :path.
	HeaderOrder []string `json:"header_order"`
	// PriorityFrame indicates whether to send a PRIORITY frame for
	// stream 0 (Chromium sends one with weight 256).
	PriorityFrame bool `json:"priority_frame"`
}

// DefaultH2FingerprintConfig returns the Chromium-matching H2
// fingerprint configuration (spec L4099).
func DefaultH2FingerprintConfig() H2FingerprintConfig {
	return H2FingerprintConfig{
		Settings:      ChromiumH2Settings(),
		WindowUpdate:  15663105,
		HeaderOrder:   []string{":method", ":authority", ":scheme", ":path"},
		PriorityFrame: true,
	}
}

// Validate checks if the H2 fingerprint config matches Chromium's
// fingerprint (spec L4099: MUST match real Chromium build).
func (c H2FingerprintConfig) Validate() error {
	if err := ValidateH2Settings(c.Settings); err != nil {
		return err
	}
	if c.WindowUpdate == 0 {
		return fmt.Errorf("h2 fingerprint: window_update must be non-zero")
	}
	if len(c.HeaderOrder) == 0 {
		return fmt.Errorf("h2 fingerprint: header_order must be non-empty")
	}
	// Chromium sends :method, :authority, :scheme, :path.
	expectedHeaders := []string{":method", ":authority", ":scheme", ":path"}
	if len(c.HeaderOrder) < len(expectedHeaders) {
		return fmt.Errorf("h2 fingerprint: header_order has %d entries, expected at least %d", len(c.HeaderOrder), len(expectedHeaders))
	}
	for i, exp := range expectedHeaders {
		if c.HeaderOrder[i] != exp {
			return fmt.Errorf("h2 fingerprint: header_order[%d] is %s, expected %s", i, c.HeaderOrder[i], exp)
		}
	}
	return nil
}

// FingerprintHash returns a deterministic hash string for the H2
// fingerprint config, useful for logging and diagnostics.
func (c H2FingerprintConfig) FingerprintHash() string {
	var b strings.Builder
	for _, s := range c.Settings.Settings {
		b.WriteString(fmt.Sprintf("%x=%d;", uint16(s.ID), s.Value))
	}
	b.WriteString(fmt.Sprintf("wu=%d;", c.WindowUpdate))
	b.WriteString(fmt.Sprintf("pf=%v;", c.PriorityFrame))
	b.WriteString("ho=")
	b.WriteString(strings.Join(c.HeaderOrder, ","))
	return b.String()
}

// IsChromiumFingerprint checks if a config matches the default
// Chromium H2 fingerprint (spec L4099).
func IsChromiumFingerprint(config H2FingerprintConfig) bool {
	return ValidateH2SettingsStrict(config.Settings) == nil &&
		config.WindowUpdate == 15663105 &&
		config.PriorityFrame == true
}
