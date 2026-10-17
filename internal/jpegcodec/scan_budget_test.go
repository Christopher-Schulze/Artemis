package jpegcodec

import (
	"bytes"
	"image"
	"image/jpeg"
	"strings"
	"testing"
)

func TestJPEGScanBudgetParsesMetadataStuffingAndRestartMarkers(t *testing.T) {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 32, 24)), nil); err != nil {
		t.Fatal(err)
	}
	raw := encoded.Bytes()
	if err := ValidateScanBudget(raw); err != nil {
		t.Fatal(err)
	}
	// Valid COM metadata includes 101 scan-looking byte pairs. jpeg.Decode
	// accepts it and the structural budget must not treat them as scans.
	comment := append([]byte{0xff, 0xfe, 0, 204}, bytes.Repeat([]byte{0xff, 0xda}, 101)...)
	metadata := append(append(append([]byte(nil), raw[:2]...), comment...), raw[2:]...)
	if _, err := jpeg.Decode(bytes.NewReader(metadata)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateScanBudget(metadata); err != nil {
		t.Fatalf("metadata counted as scans: %v", err)
	}
	for _, tc := range []struct {
		name  string
		raw   []byte
		valid bool
	}{
		{"stuffed entropy", []byte{0xff, 0xd8, 0xff, 0xda, 0, 2, 0xff, 0, 0xff, 0xd9}, true},
		{"restart entropy", []byte{0xff, 0xd8, 0xff, 0xda, 0, 2, 0xff, 0xd0, 0xff, 0xd9}, true},
		{"stuffing outside entropy", []byte{0xff, 0xd8, 0xff, 0, 0xff, 0xd9}, false},
		{"missing end", raw[:len(raw)-2], false},
		{"short segment", []byte{0xff, 0xd8, 0xff, 0xfe, 0, 1, 0xff, 0xd9}, false},
		{"long segment", []byte{0xff, 0xd8, 0xff, 0xfe, 0xff, 0xff, 0xff, 0xd9}, false},
		{"no scans", []byte{0xff, 0xd8, 0xff, 0xd9}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateScanBudget(tc.raw); (err == nil) != tc.valid {
				t.Fatalf("marker parser: %v", err)
			}
		})
	}
}

func TestJPEGScanBudgetRejectsExcessiveScans(t *testing.T) {
	for _, scans := range []int{100, 101} {
		raw := []byte{0xff, 0xd8}
		for i := 0; i < scans; i++ {
			raw = append(raw, 0xff, 0xda, 0, 2, 0)
		}
		raw = append(raw, 0xff, 0xd9)
		err := ValidateScanBudget(raw)
		if scans == 100 && err != nil {
			t.Fatal(err)
		}
		if scans == 101 && (err == nil || !strings.Contains(err.Error(), "100-scan")) {
			t.Fatalf("work limit did not reject the 101st real scan marker: %v", err)
		}
	}
}
