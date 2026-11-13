package network

import (
	"testing"
)

// ==================== H2SettingID tests ====================

// TestH2SettingIDConstants verifies the setting ID constants
// (HEADER_TABLE_SIZE, ENABLE_PUSH, MAX_CONCURRENT,
// INITIAL_WINDOW, MAX_FRAME, MAX_HEADER).
func TestH2SettingIDConstants(t *testing.T) {
	if H2SettingHeaderTableSize != 1 {
		t.Error("HEADER_TABLE_SIZE should be ID=1")
	}
	if H2SettingEnablePush != 2 {
		t.Error("ENABLE_PUSH should be ID=2")
	}
	if H2SettingMaxConcurrentStreams != 3 {
		t.Error("MAX_CONCURRENT_STREAMS should be ID=3")
	}
	if H2SettingInitialWindowSize != 4 {
		t.Error("INITIAL_WINDOW_SIZE should be ID=4")
	}
	if H2SettingMaxFrameSize != 5 {
		t.Error("MAX_FRAME_SIZE should be ID=5")
	}
	if H2SettingMaxHeaderListSize != 6 {
		t.Error("MAX_HEADER_LIST_SIZE should be ID=6")
	}
}

// TestH2SettingIDString verifies the String method.
func TestH2SettingIDString(t *testing.T) {
	cases := []struct {
		id   H2SettingID
		name string
	}{
		{H2SettingHeaderTableSize, "HEADER_TABLE_SIZE"},
		{H2SettingEnablePush, "ENABLE_PUSH"},
		{H2SettingMaxConcurrentStreams, "MAX_CONCURRENT_STREAMS"},
		{H2SettingInitialWindowSize, "INITIAL_WINDOW_SIZE"},
		{H2SettingMaxFrameSize, "MAX_FRAME_SIZE"},
		{H2SettingMaxHeaderListSize, "MAX_HEADER_LIST_SIZE"},
	}
	for _, c := range cases {
		if c.id.String() != c.name {
			t.Errorf("ID %d String: got %s, want %s", c.id, c.id.String(), c.name)
		}
	}
}

// TestH2SettingIDUnknownString verifies unknown ID String.
func TestH2SettingIDUnknownString(t *testing.T) {
	id := H2SettingID(99)
	if id.String() != "UNKNOWN(99)" {
		t.Errorf("unknown ID String: got %s, want UNKNOWN(99)", id.String())
	}
}

// ==================== ChromiumH2Settings tests ====================

// TestChromiumH2Settings verifies the Chromium H2 settings
// have all 6 settings in the correct order.
func TestChromiumH2Settings(t *testing.T) {
	frame := ChromiumH2Settings()
	if len(frame.Settings) != 6 {
		t.Fatalf("expected 6 settings, got %d", len(frame.Settings))
	}
	// Verify order: 1, 2, 3, 4, 5, 6
	expectedIDs := []H2SettingID{1, 2, 3, 4, 5, 6}
	for i, id := range expectedIDs {
		if frame.Settings[i].ID != id {
			t.Errorf("setting[%d].ID: got %d, want %d", i, frame.Settings[i].ID, id)
		}
	}
}

// TestChromiumH2SettingsValues verifies the Chromium H2
// settings values.
func TestChromiumH2SettingsValues(t *testing.T) {
	frame := ChromiumH2Settings()
	expected := map[H2SettingID]uint32{
		H2SettingHeaderTableSize:      65536,
		H2SettingEnablePush:           0,
		H2SettingMaxConcurrentStreams: 1000,
		H2SettingInitialWindowSize:    6291456,
		H2SettingMaxFrameSize:         16384,
		H2SettingMaxHeaderListSize:    262144,
	}
	for id, want := range expected {
		got, ok := frame.Get(id)
		if !ok {
			t.Errorf("setting %s not found", id)
		}
		if got != want {
			t.Errorf("setting %s: got %d, want %d", id, got, want)
		}
	}
}

// ==================== Serialize tests ====================

// TestSerialize verifies serialization to Akamai format
// ("ID:VALUE;ID:VALUE;...").
func TestSerialize(t *testing.T) {
	frame := ChromiumH2Settings()
	s := frame.Serialize()
	expected := "1:65536;2:0;3:1000;4:6291456;5:16384;6:262144"
	if s != expected {
		t.Errorf("serialize: got %s, want %s", s, expected)
	}
}

// TestSerializeEmpty verifies empty frame serializes to "".
func TestSerializeEmpty(t *testing.T) {
	frame := H2SettingsFrame{}
	if frame.Serialize() != "" {
		t.Error("empty frame should serialize to empty string")
	}
}

// TestSerializeRoundTrip verifies serialize+parse round-trip.
func TestSerializeRoundTrip(t *testing.T) {
	original := ChromiumH2Settings()
	s := original.Serialize()
	parsed, err := ParseH2SettingsFrame(s)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Serialize() != s {
		t.Error("round-trip mismatch")
	}
	if !parsed.MatchesChromium() {
		t.Error("parsed frame should match Chromium")
	}
}

// ==================== ParseH2SettingsFrame tests ====================

// TestParseValid verifies parsing a valid fingerprint string.
func TestParseValid(t *testing.T) {
	frame, err := ParseH2SettingsFrame("1:65536;2:0;3:1000;4:6291456;5:16384;6:262144")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(frame.Settings) != 6 {
		t.Fatalf("expected 6 settings, got %d", len(frame.Settings))
	}
	if frame.Settings[0].ID != H2SettingHeaderTableSize || frame.Settings[0].Value != 65536 {
		t.Error("first setting mismatch")
	}
}

// TestParseEmpty verifies parsing empty string errors.
func TestParseEmpty(t *testing.T) {
	_, err := ParseH2SettingsFrame("")
	if err == nil {
		t.Fatal("empty string should error")
	}
}

// TestParseMalformed verifies parsing malformed string errors.
func TestParseMalformed(t *testing.T) {
	_, err := ParseH2SettingsFrame("1:65536;malformed;3:1000")
	if err == nil {
		t.Fatal("malformed string should error")
	}
}

// TestParseInvalidNumber verifies parsing invalid number errors.
func TestParseInvalidNumber(t *testing.T) {
	_, err := ParseH2SettingsFrame("1:abc;2:0")
	if err == nil {
		t.Fatal("invalid number should error")
	}
}

// TestParseSingleSetting verifies parsing a single setting.
func TestParseSingleSetting(t *testing.T) {
	frame, err := ParseH2SettingsFrame("1:4096")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(frame.Settings) != 1 {
		t.Fatalf("expected 1 setting, got %d", len(frame.Settings))
	}
	if frame.Settings[0].ID != H2SettingHeaderTableSize || frame.Settings[0].Value != 4096 {
		t.Error("setting mismatch")
	}
}

// ==================== MatchesChromium tests ====================

// TestMatchesChromiumTrue verifies Chromium settings match.
func TestMatchesChromiumTrue(t *testing.T) {
	frame := ChromiumH2Settings()
	if !frame.MatchesChromium() {
		t.Error("Chromium settings should match Chromium")
	}
}

// TestMatchesChromiumFalse verifies wrong values don't match.
func TestMatchesChromiumFalse(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingHeaderTableSize, 4096}, // wrong value
			{H2SettingEnablePush, 0},
			{H2SettingMaxConcurrentStreams, 1000},
			{H2SettingInitialWindowSize, 6291456},
			{H2SettingMaxFrameSize, 16384},
			{H2SettingMaxHeaderListSize, 262144},
		},
	}
	if frame.MatchesChromium() {
		t.Error("wrong values should NOT match Chromium")
	}
}

// TestMatchesChromiumWrongOrder verifies wrong order doesn't
// match (frame order MUST match).
func TestMatchesChromiumWrongOrder(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingEnablePush, 0}, // wrong order (should be 2nd)
			{H2SettingHeaderTableSize, 65536},
			{H2SettingMaxConcurrentStreams, 1000},
			{H2SettingInitialWindowSize, 6291456},
			{H2SettingMaxFrameSize, 16384},
			{H2SettingMaxHeaderListSize, 262144},
		},
	}
	if frame.MatchesChromium() {
		t.Error("wrong order should NOT match Chromium")
	}
}

// TestMatchesChromiumWrongCount verifies wrong count doesn't
// match.
func TestMatchesChromiumWrongCount(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingHeaderTableSize, 65536},
			{H2SettingEnablePush, 0},
		},
	}
	if frame.MatchesChromium() {
		t.Error("wrong count should NOT match Chromium")
	}
}

// ==================== Validate tests ====================

// TestValidateValid verifies valid frame passes validation.
func TestValidateValid(t *testing.T) {
	frame := ChromiumH2Settings()
	if err := frame.Validate(); err != nil {
		t.Errorf("valid frame should pass: %v", err)
	}
}

// TestValidateEmpty verifies empty frame fails validation.
func TestValidateEmpty(t *testing.T) {
	frame := H2SettingsFrame{}
	if err := frame.Validate(); err == nil {
		t.Fatal("empty frame should fail validation")
	}
}

// TestValidateInvalidID verifies invalid ID fails validation.
func TestValidateInvalidID(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingID(99), 100},
		},
	}
	if err := frame.Validate(); err == nil {
		t.Fatal("invalid ID should fail validation")
	}
}

// TestValidateDuplicateID verifies duplicate ID fails
// validation.
func TestValidateDuplicateID(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingHeaderTableSize, 65536},
			{H2SettingHeaderTableSize, 4096}, // duplicate
		},
	}
	if err := frame.Validate(); err == nil {
		t.Fatal("duplicate ID should fail validation")
	}
}

// TestValidateEnablePushInvalid verifies ENABLE_PUSH > 1
// fails validation (RFC 7540: must be 0 or 1).
func TestValidateEnablePushInvalid(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingEnablePush, 2},
		},
	}
	if err := frame.Validate(); err == nil {
		t.Fatal("ENABLE_PUSH=2 should fail validation")
	}
}

// TestValidateMaxFrameSizeTooSmall verifies MAX_FRAME_SIZE
// < 16384 fails validation (RFC 7540: min 2^14).
func TestValidateMaxFrameSizeTooSmall(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingMaxFrameSize, 1000},
		},
	}
	if err := frame.Validate(); err == nil {
		t.Fatal("MAX_FRAME_SIZE < 16384 should fail validation")
	}
}

// TestValidateMaxFrameSizeTooLarge verifies MAX_FRAME_SIZE
// > 16777215 fails validation (RFC 7540: max 2^24-1).
func TestValidateMaxFrameSizeTooLarge(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingMaxFrameSize, 99999999},
		},
	}
	if err := frame.Validate(); err == nil {
		t.Fatal("MAX_FRAME_SIZE > 16777215 should fail validation")
	}
}

// TestValidateInitialWindowTooLarge verifies
// INITIAL_WINDOW_SIZE > 2^31-1 fails validation (RFC 7540).
func TestValidateInitialWindowTooLarge(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingInitialWindowSize, 3000000000},
		},
	}
	if err := frame.Validate(); err == nil {
		t.Fatal("INITIAL_WINDOW_SIZE > 2^31-1 should fail validation")
	}
}

// TestValidateEnablePushValid verifies ENABLE_PUSH=0 and =1
// pass validation.
func TestValidateEnablePushValid(t *testing.T) {
	for _, val := range []uint32{0, 1} {
		frame := H2SettingsFrame{
			Settings: []H2Setting{
				{H2SettingEnablePush, val},
			},
		}
		if err := frame.Validate(); err != nil {
			t.Errorf("ENABLE_PUSH=%d should pass validation: %v", val, err)
		}
	}
}

// ==================== Get tests ====================

// TestGet verifies Get returns the correct value.
func TestGet(t *testing.T) {
	frame := ChromiumH2Settings()
	val, ok := frame.Get(H2SettingHeaderTableSize)
	if !ok {
		t.Fatal("HEADER_TABLE_SIZE not found")
	}
	if val != 65536 {
		t.Errorf("HEADER_TABLE_SIZE: got %d, want 65536", val)
	}
}

// TestGetNotFound verifies Get returns false for missing ID.
func TestGetNotFound(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingHeaderTableSize, 65536},
		},
	}
	_, ok := frame.Get(H2SettingEnablePush)
	if ok {
		t.Error("ENABLE_PUSH should not be found")
	}
}

// ==================== HasAllChromiumSettings tests ====================

// TestHasAllChromiumSettingsTrue verifies Chromium frame has
// all settings.
func TestHasAllChromiumSettingsTrue(t *testing.T) {
	frame := ChromiumH2Settings()
	if !frame.HasAllChromiumSettings() {
		t.Error("Chromium frame should have all settings")
	}
}

// TestHasAllChromiumSettingsFalse verifies partial frame
// doesn't have all.
func TestHasAllChromiumSettingsFalse(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingHeaderTableSize, 65536},
			{H2SettingEnablePush, 0},
		},
	}
	if frame.HasAllChromiumSettings() {
		t.Error("partial frame should NOT have all settings")
	}
}

// ==================== OrderMatchesChromium tests ====================

// TestOrderMatchesChromiumTrue verifies Chromium order matches.
func TestOrderMatchesChromiumTrue(t *testing.T) {
	frame := ChromiumH2Settings()
	if !frame.OrderMatchesChromium() {
		t.Error("Chromium frame order should match")
	}
}

// TestOrderMatchesChromiumFalse verifies wrong order doesn't
// match (frame order MUST match).
func TestOrderMatchesChromiumFalse(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingMaxHeaderListSize, 262144}, // wrong order
			{H2SettingHeaderTableSize, 65536},
			{H2SettingEnablePush, 0},
			{H2SettingMaxConcurrentStreams, 1000},
			{H2SettingInitialWindowSize, 6291456},
			{H2SettingMaxFrameSize, 16384},
		},
	}
	if frame.OrderMatchesChromium() {
		t.Error("wrong order should NOT match Chromium")
	}
}

// TestOrderMatchesChromiumDifferentValues verifies order
// matches even with different values (order check is ID-only).
func TestOrderMatchesChromiumDifferentValues(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingHeaderTableSize, 999}, // different value
			{H2SettingEnablePush, 0},
			{H2SettingMaxConcurrentStreams, 1000},
			{H2SettingInitialWindowSize, 6291456},
			{H2SettingMaxFrameSize, 16384},
			{H2SettingMaxHeaderListSize, 262144},
		},
	}
	if !frame.OrderMatchesChromium() {
		t.Error("order should match even with different values")
	}
}

// ==================== integration with TLSFingerprint tests ====================

// TestTLSFingerprintH2Consistency verifies that the
// TLSFingerprint.HTTP2Fingerprint string matches the structured
// H2SettingsFrame serialization.
func TestTLSFingerprintH2Consistency(t *testing.T) {
	fp := Chrome145Fingerprint()
	frame := ChromiumH2Settings()
	if fp.HTTP2Fingerprint != frame.Serialize() {
		t.Errorf("TLSFingerprint.HTTP2Fingerprint (%s) != H2SettingsFrame.Serialize() (%s)",
			fp.HTTP2Fingerprint, frame.Serialize())
	}
}

// TestTLSFingerprintH2ParseAndVerify verifies the
// HTTP2Fingerprint string can be parsed and matches Chromium.
func TestTLSFingerprintH2ParseAndVerify(t *testing.T) {
	fp := Chrome145Fingerprint()
	frame, err := ParseH2SettingsFrame(fp.HTTP2Fingerprint)
	if err != nil {
		t.Fatalf("parse HTTP2Fingerprint: %v", err)
	}
	if !frame.MatchesChromium() {
		t.Error("parsed HTTP2Fingerprint should match Chromium")
	}
}

// ==================== parity test ====================

// TestH2FingerprintParity verifies full parity.
// (H2 settings frame order matches real Chromium build).
func TestH2FingerprintParity(t *testing.T) {
	// 1. All 6 setting IDs exist
	ids := []H2SettingID{
		H2SettingHeaderTableSize,
		H2SettingEnablePush,
		H2SettingMaxConcurrentStreams,
		H2SettingInitialWindowSize,
		H2SettingMaxFrameSize,
		H2SettingMaxHeaderListSize,
	}
	for _, id := range ids {
		if id < 1 || id > 6 {
			t.Errorf("setting ID %d out of range", id)
		}
	}

	// 2. Chromium settings have correct order
	frame := ChromiumH2Settings()
	if !frame.OrderMatchesChromium() {
		t.Error("Chromium settings should have correct order")
	}

	// 3. Chromium settings match themselves
	if !frame.MatchesChromium() {
		t.Error("Chromium settings should match themselves")
	}

	// 4. Serialize to Akamai format
	s := frame.Serialize()
	if s == "" {
		t.Error("serialize should not be empty")
	}

	// 5. Parse back
	parsed, err := ParseH2SettingsFrame(s)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// 6. Parsed frame matches Chromium
	if !parsed.MatchesChromium() {
		t.Error("parsed frame should match Chromium")
	}

	// 7. Validate passes
	if err := frame.Validate(); err != nil {
		t.Errorf("validate: %v", err)
	}

	// 8. Has all Chromium settings
	if !frame.HasAllChromiumSettings() {
		t.Error("should have all Chromium settings")
	}

	// 9. Consistency with TLSFingerprint
	fp := Chrome145Fingerprint()
	if fp.HTTP2Fingerprint != frame.Serialize() {
		t.Error("TLSFingerprint.HTTP2Fingerprint should match H2SettingsFrame")
	}
}
