package bridge

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

func TestScreenshotBase64ParityAndBounds(t *testing.T) {
	for _, size := range []int{1, 2, 3, 4, 7, 8, 63, 64, 65, 255, 256, 257, 4096, 65537} {
		raw := make([]byte, size)
		for index := range raw {
			raw[index] = byte(index * 37)
		}
		encoded := base64.StdEncoding.EncodeToString(raw)
		for _, value := range []string{encoded, "\r\n" + encoded[:4] + "\n" + encoded[4:]} {
			decoded, err := DecodeScreenshotData(value)
			if err != nil || !bytes.Equal(decoded, raw) {
				t.Fatalf("size %d parity: %v", size, err)
			}
		}
	}
	for _, encoded := range []string{"", "%%%%", "A", "AAA", "AA=A", "=AAA", "AAAA====", strings.Repeat("A", JPEGEncodedLimit+1)} {
		decoded, err := DecodeScreenshotData(encoded)
		if err == nil || decoded != nil {
			t.Fatalf("malformed/oversized input admitted: %q %v", encoded[:min(len(encoded), 16)], err)
		}
	}
}

func TestScreenshotBase64CallerBufferOwnershipAndZeroAllocation(t *testing.T) {
	raw := bytes.Repeat([]byte{0, 13, 10, 255, 73}, 1024)
	encoded := base64.StdEncoding.EncodeToString(raw)
	fingerprint := sha256.Sum256([]byte(encoded))
	buffer := make([]byte, base64.StdEncoding.DecodedLen(len(encoded))+1)
	n, err := DecodeScreenshotDataInto(buffer, encoded)
	if err != nil || !bytes.Equal(buffer[:n], raw) {
		t.Fatalf("caller buffer corrupted: %d %v", n, err)
	}
	if sha256.Sum256([]byte(encoded)) != fingerprint {
		t.Fatal("immutable source changed")
	}
	if allocations := testing.AllocsPerRun(100, func() {
		if _, err := DecodeScreenshotDataInto(buffer, encoded); err != nil {
			t.Fatal(err)
		}
	}); allocations != 0 {
		t.Fatalf("caller buffer allocated: %v", allocations)
	}
	if n, err := DecodeScreenshotDataInto(buffer[:len(raw)-1], encoded); err == nil || n != 0 {
		t.Fatalf("short output buffer admitted: %d %v", n, err)
	}
}

func BenchmarkScreenshotBase64Decoder(b *testing.B) {
	raw := make([]byte, 256<<10)
	for index := range raw {
		raw[index] = byte(index * 37)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	for _, tc := range []struct {
		name   string
		decode func(string) ([]byte, error)
	}{{"Stdlib", base64.StdEncoding.DecodeString}, {"Production", DecodeScreenshotData}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(raw)))
			for b.Loop() {
				decoded, err := tc.decode(encoded)
				if err != nil || len(decoded) != len(raw) || decoded[0] != raw[0] || decoded[len(raw)-1] != raw[len(raw)-1] {
					b.Fatalf("decode corrupted output: %v", err)
				}
			}
		})
	}
}

func FuzzScreenshotBase64Parity(f *testing.F) {
	for _, value := range []string{"AA==", "AAAA", "AA=A", "AAAA====", "\r\nYWJj\n", "aGVsbG8=", "%%%", "A"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, encoded string) {
		if len(encoded) == 0 || len(encoded) > JPEGEncodedLimit {
			return
		}
		want, referenceErr := base64.StdEncoding.DecodeString(encoded)
		got, err := DecodeScreenshotData(encoded)
		if referenceErr != nil {
			if err == nil {
				t.Fatalf("stdlib-invalid input admitted: %q", encoded)
			}
			return
		}
		if len(want) == 0 {
			return
		}
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("stdlib parity failed: %q %v", encoded, err)
		}
	})
}
