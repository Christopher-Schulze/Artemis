package stealth

import (
	"testing"
)

func TestH2SettingIDs(t *testing.T) {
	if H2SettingHeaderTableSize != 0x1 {
		t.Errorf("expected 0x1, got 0x%x", uint16(H2SettingHeaderTableSize))
	}
	if H2SettingEnablePush != 0x2 {
		t.Errorf("expected 0x2, got 0x%x", uint16(H2SettingEnablePush))
	}
	if H2SettingMaxConcurrentStreams != 0x3 {
		t.Errorf("expected 0x3, got 0x%x", uint16(H2SettingMaxConcurrentStreams))
	}
	if H2SettingInitialWindowSize != 0x4 {
		t.Errorf("expected 0x4, got 0x%x", uint16(H2SettingInitialWindowSize))
	}
	if H2SettingMaxFrameSize != 0x5 {
		t.Errorf("expected 0x5, got 0x%x", uint16(H2SettingMaxFrameSize))
	}
	if H2SettingMaxHeaderListSize != 0x6 {
		t.Errorf("expected 0x6, got 0x%x", uint16(H2SettingMaxHeaderListSize))
	}
}

func TestH2Setting_String(t *testing.T) {
	tests := []struct {
		id   H2SettingID
		want string
	}{
		{H2SettingHeaderTableSize, "HEADER_TABLE_SIZE"},
		{H2SettingEnablePush, "ENABLE_PUSH"},
		{H2SettingMaxConcurrentStreams, "MAX_CONCURRENT_STREAMS"},
		{H2SettingInitialWindowSize, "INITIAL_WINDOW_SIZE"},
		{H2SettingMaxFrameSize, "MAX_FRAME_SIZE"},
		{H2SettingMaxHeaderListSize, "MAX_HEADER_LIST_SIZE"},
		{H2SettingEnableConnectProtocol, "ENABLE_CONNECT_PROTOCOL"},
		{H2SettingID(0x99), "UNKNOWN(0x99)"},
	}
	for _, tt := range tests {
		s := H2Setting{ID: tt.id}
		if s.String() != tt.want {
			t.Errorf("expected %s, got %s", tt.want, s.String())
		}
	}
}

func TestChromiumH2Settings(t *testing.T) {
	frame := ChromiumH2Settings()
	if len(frame.Settings) != 6 {
		t.Fatalf("expected 6 settings, got %d", len(frame.Settings))
	}

	// Verify order matches spec L4099.
	expectedOrder := []H2SettingID{
		H2SettingHeaderTableSize,
		H2SettingEnablePush,
		H2SettingMaxConcurrentStreams,
		H2SettingInitialWindowSize,
		H2SettingMaxFrameSize,
		H2SettingMaxHeaderListSize,
	}
	for i, expectedID := range expectedOrder {
		if frame.Settings[i].ID != expectedID {
			t.Errorf("position %d: expected %s, got %s", i,
				H2Setting{ID: expectedID}.String(),
				frame.Settings[i].String())
		}
	}
}

func TestChromiumH2Settings_Values(t *testing.T) {
	frame := ChromiumH2Settings()

	// Verify Chromium values.
	expectedValues := map[H2SettingID]uint32{
		H2SettingHeaderTableSize:      65536,
		H2SettingEnablePush:           0,
		H2SettingMaxConcurrentStreams: 1000,
		H2SettingInitialWindowSize:    6291456,
		H2SettingMaxFrameSize:         16384,
		H2SettingMaxHeaderListSize:    262144,
	}
	for _, s := range frame.Settings {
		if s.Value != expectedValues[s.ID] {
			t.Errorf("%s: expected %d, got %d", s.String(), expectedValues[s.ID], s.Value)
		}
	}
}

func TestChromiumH2SettingsOrder(t *testing.T) {
	order := ChromiumH2SettingsOrder()
	if len(order) != 6 {
		t.Fatalf("expected 6 IDs, got %d", len(order))
	}
	expected := []H2SettingID{
		H2SettingHeaderTableSize,
		H2SettingEnablePush,
		H2SettingMaxConcurrentStreams,
		H2SettingInitialWindowSize,
		H2SettingMaxFrameSize,
		H2SettingMaxHeaderListSize,
	}
	for i, id := range order {
		if id != expected[i] {
			t.Errorf("position %d: expected 0x%x, got 0x%x", i, uint16(expected[i]), uint16(id))
		}
	}
}

func TestValidateH2Settings_CorrectOrder(t *testing.T) {
	frame := ChromiumH2Settings()
	if err := ValidateH2Settings(frame); err != nil {
		t.Fatalf("expected nil error for Chromium order, got: %v", err)
	}
}

func TestValidateH2Settings_WrongOrder(t *testing.T) {
	// Swap first two settings.
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{ID: H2SettingEnablePush, Value: 0},
			{ID: H2SettingHeaderTableSize, Value: 65536},
			{ID: H2SettingMaxConcurrentStreams, Value: 1000},
			{ID: H2SettingInitialWindowSize, Value: 6291456},
			{ID: H2SettingMaxFrameSize, Value: 16384},
			{ID: H2SettingMaxHeaderListSize, Value: 262144},
		},
	}
	err := ValidateH2Settings(frame)
	if err == nil {
		t.Fatal("expected error for wrong order")
	}
}

func TestValidateH2Settings_TooFewSettings(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{ID: H2SettingHeaderTableSize, Value: 65536},
			{ID: H2SettingEnablePush, Value: 0},
		},
	}
	err := ValidateH2Settings(frame)
	if err == nil {
		t.Fatal("expected error for too few settings")
	}
}

func TestValidateH2Settings_ExtraSettingsOK(t *testing.T) {
	// Extra settings after the Chromium 6 are OK (order check only
	// validates the first 6).
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{ID: H2SettingHeaderTableSize, Value: 65536},
			{ID: H2SettingEnablePush, Value: 0},
			{ID: H2SettingMaxConcurrentStreams, Value: 1000},
			{ID: H2SettingInitialWindowSize, Value: 6291456},
			{ID: H2SettingMaxFrameSize, Value: 16384},
			{ID: H2SettingMaxHeaderListSize, Value: 262144},
			{ID: H2SettingEnableConnectProtocol, Value: 1},
		},
	}
	err := ValidateH2Settings(frame)
	if err != nil {
		t.Fatalf("expected nil error for extra settings, got: %v", err)
	}
}

func TestValidateH2Settings_EmptyFrame(t *testing.T) {
	frame := H2SettingsFrame{}
	err := ValidateH2Settings(frame)
	if err == nil {
		t.Fatal("expected error for empty frame")
	}
}

func TestValidateH2SettingsStrict_Correct(t *testing.T) {
	frame := ChromiumH2Settings()
	if err := ValidateH2SettingsStrict(frame); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestValidateH2SettingsStrict_WrongValue(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{ID: H2SettingHeaderTableSize, Value: 4096}, // wrong value
			{ID: H2SettingEnablePush, Value: 0},
			{ID: H2SettingMaxConcurrentStreams, Value: 1000},
			{ID: H2SettingInitialWindowSize, Value: 6291456},
			{ID: H2SettingMaxFrameSize, Value: 16384},
			{ID: H2SettingMaxHeaderListSize, Value: 262144},
		},
	}
	err := ValidateH2SettingsStrict(frame)
	if err == nil {
		t.Fatal("expected error for wrong value")
	}
}

func TestValidateH2SettingsStrict_ExtraSettings(t *testing.T) {
	frame := H2SettingsFrame{
		Settings: []H2Setting{
			{ID: H2SettingHeaderTableSize, Value: 65536},
			{ID: H2SettingEnablePush, Value: 0},
			{ID: H2SettingMaxConcurrentStreams, Value: 1000},
			{ID: H2SettingInitialWindowSize, Value: 6291456},
			{ID: H2SettingMaxFrameSize, Value: 16384},
			{ID: H2SettingMaxHeaderListSize, Value: 262144},
			{ID: H2SettingEnableConnectProtocol, Value: 1},
		},
	}
	err := ValidateH2SettingsStrict(frame)
	if err == nil {
		t.Fatal("expected error for extra settings in strict mode")
	}
}

func TestH2SettingsFrame_IDs(t *testing.T) {
	frame := ChromiumH2Settings()
	ids := frame.IDs()
	if len(ids) != 6 {
		t.Fatalf("expected 6 IDs, got %d", len(ids))
	}
	if ids[0] != H2SettingHeaderTableSize {
		t.Errorf("expected first ID=HEADER_TABLE_SIZE, got 0x%x", uint16(ids[0]))
	}
}

func TestH2SettingsFrame_Names(t *testing.T) {
	frame := ChromiumH2Settings()
	names := frame.Names()
	if len(names) != 6 {
		t.Fatalf("expected 6 names, got %d", len(names))
	}
	if names[0] != "HEADER_TABLE_SIZE" {
		t.Errorf("expected first name=HEADER_TABLE_SIZE, got %s", names[0])
	}
	if names[5] != "MAX_HEADER_LIST_SIZE" {
		t.Errorf("expected last name=MAX_HEADER_LIST_SIZE, got %s", names[5])
	}
}

func TestDefaultH2FingerprintConfig(t *testing.T) {
	config := DefaultH2FingerprintConfig()
	if config.WindowUpdate != 15663105 {
		t.Errorf("expected window_update=15663105, got %d", config.WindowUpdate)
	}
	if !config.PriorityFrame {
		t.Error("expected priority_frame=true")
	}
	if len(config.HeaderOrder) != 4 {
		t.Fatalf("expected 4 header_order entries, got %d", len(config.HeaderOrder))
	}
	expectedHeaders := []string{":method", ":authority", ":scheme", ":path"}
	for i, h := range expectedHeaders {
		if config.HeaderOrder[i] != h {
			t.Errorf("header_order[%d]: expected %s, got %s", i, h, config.HeaderOrder[i])
		}
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestH2FingerprintConfig_Validate(t *testing.T) {
	config := DefaultH2FingerprintConfig()
	if err := config.Validate(); err != nil {
		t.Fatalf("expected nil error for default config, got: %v", err)
	}
}

func TestH2FingerprintConfig_Validate_WrongOrder(t *testing.T) {
	config := DefaultH2FingerprintConfig()
	config.Settings.Settings[0], config.Settings.Settings[1] =
		config.Settings.Settings[1], config.Settings.Settings[0]
	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for wrong settings order")
	}
}

func TestH2FingerprintConfig_Validate_ZeroWindowUpdate(t *testing.T) {
	config := DefaultH2FingerprintConfig()
	config.WindowUpdate = 0
	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for zero window_update")
	}
}

func TestH2FingerprintConfig_Validate_EmptyHeaderOrder(t *testing.T) {
	config := DefaultH2FingerprintConfig()
	config.HeaderOrder = []string{}
	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for empty header_order")
	}
}

func TestH2FingerprintConfig_Validate_WrongHeaderOrder(t *testing.T) {
	config := DefaultH2FingerprintConfig()
	config.HeaderOrder = []string{":path", ":method", ":authority", ":scheme"}
	err := config.Validate()
	if err == nil {
		t.Fatal("expected error for wrong header_order")
	}
}

func TestH2FingerprintConfig_FingerprintHash(t *testing.T) {
	config := DefaultH2FingerprintConfig()
	hash := config.FingerprintHash()
	if hash == "" {
		t.Fatal("expected non-empty fingerprint hash")
	}
	// Hash should be deterministic.
	config2 := DefaultH2FingerprintConfig()
	if config.FingerprintHash() != config2.FingerprintHash() {
		t.Fatal("expected deterministic fingerprint hash")
	}
}

func TestH2FingerprintConfig_FingerprintHash_DifferentConfigs(t *testing.T) {
	config1 := DefaultH2FingerprintConfig()
	config2 := DefaultH2FingerprintConfig()
	config2.WindowUpdate = 99999
	if config1.FingerprintHash() == config2.FingerprintHash() {
		t.Fatal("expected different fingerprint hashes for different configs")
	}
}

func TestIsChromiumFingerprint(t *testing.T) {
	config := DefaultH2FingerprintConfig()
	if !IsChromiumFingerprint(config) {
		t.Fatal("expected default config to be Chromium fingerprint")
	}
}

func TestIsChromiumFingerprint_WrongWindowUpdate(t *testing.T) {
	config := DefaultH2FingerprintConfig()
	config.WindowUpdate = 99999
	if IsChromiumFingerprint(config) {
		t.Fatal("expected non-Chromium fingerprint for wrong window_update")
	}
}

func TestIsChromiumFingerprint_NoPriorityFrame(t *testing.T) {
	config := DefaultH2FingerprintConfig()
	config.PriorityFrame = false
	if IsChromiumFingerprint(config) {
		t.Fatal("expected non-Chromium fingerprint for no priority_frame")
	}
}

func TestIsChromiumFingerprint_WrongSettings(t *testing.T) {
	config := DefaultH2FingerprintConfig()
	config.Settings.Settings[0].Value = 4096
	if IsChromiumFingerprint(config) {
		t.Fatal("expected non-Chromium fingerprint for wrong settings value")
	}
}

func TestH2Setting_Value(t *testing.T) {
	s := H2Setting{ID: H2SettingHeaderTableSize, Value: 65536}
	if s.Value != 65536 {
		t.Errorf("expected 65536, got %d", s.Value)
	}
}
