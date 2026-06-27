package network

import (
	"testing"
)

// ==================== H2SettingID tests ====================

// TestTASK2241_H2SettingIDConstants verifies the setting ID constants
// (spec L4099: HEADER_TABLE_SIZE, ENABLE_PUSH, MAX_CONCURRENT,
// INITIAL_WINDOW, MAX_FRAME, MAX_HEADER).
func TestTASK2241_H2SettingIDConstants(t *testing.T) {
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

// TestTASK2241_H2SettingIDString verifies the String method
// (spec L4099).
func TestTASK2241_H2SettingIDString(t *testing.T) {
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

// TestTASK2241_H2SettingIDUnknownString verifies unknown ID String.
func TestTASK2241_H2SettingIDUnknownString(t *testing.T) {
	id := H2SettingID(99)
	if id.String() != "UNKNOWN(99)" {
		t.Errorf("unknown ID String: got %s, want UNKNOWN(99)", id.String())
	}
}

// ==================== ChromiumH2Settings tests ====================

// TestTASK2241_ChromiumH2Settings verifies the Chromium H2 settings
// have all 6 settings in the correct order (spec L4099).
func TestTASK2241_ChromiumH2Settings(t *testing.T) {
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

// TestTASK2241_ChromiumH2SettingsValues verifies the Chromium H2
// settings values (spec L4099).
func TestTASK2241_ChromiumH2SettingsValues(t *testing.T) {
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

// TestTASK2241_Serialize verifies serialization to Akamai format
// (spec L4099: "ID:VALUE;ID:VALUE;...").
func TestTASK2241_Serialize(t *testing.T) {
	frame := ChromiumH2Settings()
	s := frame.Serialize()
	expected := "1:65536;2:0;3:1000;4:6291456;5:16384;6:262144"
	if s != expected {
		t.Errorf("serialize: got %s, want %s", s, expected)
	}
}

// TestTASK2241_SerializeEmpty verifies empty frame serializes to "".
func TestTASK2241_SerializeEmpty(t *testing.T) {
	frame := H2SettingsFrame{}
	if frame.Serialize() != "" {
		t.Error("empty frame should serialize to empty string")
	}
}

// TestTASK2241_SerializeRoundTrip verifies serialize+parse round-trip.
func TestTASK2241_SerializeRoundTrip(t *testing.T) {
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

// TestTASK2241_ParseValid verifies parsing a valid fingerprint string
// (spec L4099).
func TestTASK2241_ParseValid(t *testing.T) {
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

// TestTASK2241_ParseEmpty verifies parsing empty string errors.
func TestTASK2241_ParseEmpty(t *testing.T) {
	_, err := ParseH2SettingsFrame("")
	if err == nil {
		t.Fatal("empty string should error")
	}
}

// TestTASK2241_ParseMalformed verifies parsing malformed string errors.
func TestTASK2241_ParseMalformed(t *testing.T) {
	_, err := ParseH2SettingsFrame("1:65536;malformed;3:1000")
	if err == nil {
		t.Fatal("malformed string should error")
	}
}

// TestTASK2241_ParseInvalidNumber verifies parsing invalid number errors.
func TestTASK2241_ParseInvalidNumber(t *testing.T) {
	_, err := ParseH2SettingsFrame("1:abc;2:0")
	if err == nil {
		t.Fatal("invalid number should error")
	}
}

// TestTASK2241_ParseSingleSetting verifies parsing a single setting.
func TestTASK2241_ParseSingleSetting(t *testing.T) {
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

// TestTASK2241_MatchesChromiumTrue verifies Chromium settings match.
func TestTASK2241_MatchesChromiumTrue(t *testing.T) {
	frame := ChromiumH2Settings()
	if !frame.MatchesChromium() {
		t.Error("Chromium settings should match Chromium")
	}
}

// TestTASK2241_MatchesChromiumFalse verifies wrong values don't match.
func TestTASK2241_MatchesChromiumFalse(t *testing.T) {
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

// TestTASK2241_MatchesChromiumWrongOrder verifies wrong order doesn't
// match (spec L4099: frame order MUST match).
func TestTASK2241_MatchesChromiumWrongOrder(t *testing.T) {
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

// TestTASK2241_MatchesChromiumWrongCount verifies wrong count doesn't
// match.
func TestTASK2241_MatchesChromiumWrongCount(t *testing.T) {
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

// TestTASK2241_ValidateValid verifies valid frame passes validation.
func TestTASK2241_ValidateValid(t *testing.T) {
	frame := ChromiumH2Settings()
	if err := frame.Validate(); err != nil {
		t.Errorf("valid frame should pass: %v", err)
	}
}

// TestTASK2241_ValidateEmpty verifies empty frame fails validation.
func TestTASK2241_ValidateEmpty(t *testing.T) {
	frame := H2SettingsFrame{}
	if err := frame.Validate(); err == nil {
		t.Fatal("empty frame should fail validation")
	}
}

// TestTASK2241_ValidateInvalidID verifies invalid ID fails validation.
func TestTASK2241_ValidateInvalidID(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingID(99), 100},
		},
	}
	if err := frame.Validate(); err == nil {
		t.Fatal("invalid ID should fail validation")
	}
}

// TestTASK2241_ValidateDuplicateID verifies duplicate ID fails
// validation.
func TestTASK2241_ValidateDuplicateID(t *testing.T) {
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

// TestTASK2241_ValidateEnablePushInvalid verifies ENABLE_PUSH > 1
// fails validation (RFC 7540: must be 0 or 1).
func TestTASK2241_ValidateEnablePushInvalid(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingEnablePush, 2},
		},
	}
	if err := frame.Validate(); err == nil {
		t.Fatal("ENABLE_PUSH=2 should fail validation")
	}
}

// TestTASK2241_ValidateMaxFrameSizeTooSmall verifies MAX_FRAME_SIZE
// < 16384 fails validation (RFC 7540: min 2^14).
func TestTASK2241_ValidateMaxFrameSizeTooSmall(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingMaxFrameSize, 1000},
		},
	}
	if err := frame.Validate(); err == nil {
		t.Fatal("MAX_FRAME_SIZE < 16384 should fail validation")
	}
}

// TestTASK2241_ValidateMaxFrameSizeTooLarge verifies MAX_FRAME_SIZE
// > 16777215 fails validation (RFC 7540: max 2^24-1).
func TestTASK2241_ValidateMaxFrameSizeTooLarge(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingMaxFrameSize, 99999999},
		},
	}
	if err := frame.Validate(); err == nil {
		t.Fatal("MAX_FRAME_SIZE > 16777215 should fail validation")
	}
}

// TestTASK2241_ValidateInitialWindowTooLarge verifies
// INITIAL_WINDOW_SIZE > 2^31-1 fails validation (RFC 7540).
func TestTASK2241_ValidateInitialWindowTooLarge(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{H2SettingInitialWindowSize, 3000000000},
		},
	}
	if err := frame.Validate(); err == nil {
		t.Fatal("INITIAL_WINDOW_SIZE > 2^31-1 should fail validation")
	}
}

// TestTASK2241_ValidateEnablePushValid verifies ENABLE_PUSH=0 and =1
// pass validation.
func TestTASK2241_ValidateEnablePushValid(t *testing.T) {
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

// TestTASK2241_Get verifies Get returns the correct value.
func TestTASK2241_Get(t *testing.T) {
	frame := ChromiumH2Settings()
	val, ok := frame.Get(H2SettingHeaderTableSize)
	if !ok {
		t.Fatal("HEADER_TABLE_SIZE not found")
	}
	if val != 65536 {
		t.Errorf("HEADER_TABLE_SIZE: got %d, want 65536", val)
	}
}

// TestTASK2241_GetNotFound verifies Get returns false for missing ID.
func TestTASK2241_GetNotFound(t *testing.T) {
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

// TestTASK2241_HasAllChromiumSettingsTrue verifies Chromium frame has
// all settings.
func TestTASK2241_HasAllChromiumSettingsTrue(t *testing.T) {
	frame := ChromiumH2Settings()
	if !frame.HasAllChromiumSettings() {
		t.Error("Chromium frame should have all settings")
	}
}

// TestTASK2241_HasAllChromiumSettingsFalse verifies partial frame
// doesn't have all.
func TestTASK2241_HasAllChromiumSettingsFalse(t *testing.T) {
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

// TestTASK2241_OrderMatchesChromiumTrue verifies Chromium order matches.
func TestTASK2241_OrderMatchesChromiumTrue(t *testing.T) {
	frame := ChromiumH2Settings()
	if !frame.OrderMatchesChromium() {
		t.Error("Chromium frame order should match")
	}
}

// TestTASK2241_OrderMatchesChromiumFalse verifies wrong order doesn't
// match (spec L4099: frame order MUST match).
func TestTASK2241_OrderMatchesChromiumFalse(t *testing.T) {
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

// TestTASK2241_OrderMatchesChromiumDifferentValues verifies order
// matches even with different values (order check is ID-only).
func TestTASK2241_OrderMatchesChromiumDifferentValues(t *testing.T) {
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

// TestTASK2241_TLSFingerprintH2Consistency verifies that the
// TLSFingerprint.HTTP2Fingerprint string matches the structured
// H2SettingsFrame serialization (spec L4099).
func TestTASK2241_TLSFingerprintH2Consistency(t *testing.T) {
	fp := Chrome145Fingerprint()
	frame := ChromiumH2Settings()
	if fp.HTTP2Fingerprint != frame.Serialize() {
		t.Errorf("TLSFingerprint.HTTP2Fingerprint (%s) != H2SettingsFrame.Serialize() (%s)",
			fp.HTTP2Fingerprint, frame.Serialize())
	}
}

// TestTASK2241_TLSFingerprintH2ParseAndVerify verifies the
// HTTP2Fingerprint string can be parsed and matches Chromium.
func TestTASK2241_TLSFingerprintH2ParseAndVerify(t *testing.T) {
	fp := Chrome145Fingerprint()
	frame, err := ParseH2SettingsFrame(fp.HTTP2Fingerprint)
	if err != nil {
		t.Fatalf("parse HTTP2Fingerprint: %v", err)
	}
	if !frame.MatchesChromium() {
		t.Error("parsed HTTP2Fingerprint should match Chromium")
	}
}

// ==================== full spec parity test ====================

// TestTASK2241_FullSpecParity verifies full spec parity for L4099
// (spec L4099: H2 settings frame order matches real Chromium build).
func TestTASK2241_FullSpecParity(t *testing.T) {
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
