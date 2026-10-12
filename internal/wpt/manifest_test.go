package wpt

import (
	"encoding/hex"
	"testing"
)

func TestEmbeddedAssetManifest(t *testing.T) {
	size, digest, err := EmbeddedAssetManifest()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != 32 {
		t.Fatalf("invalid manifest size=%d digest=%q", size, digest)
	}
	if size <= 0 {
		t.Fatalf("manifest size=%d", size)
	}
	t.Logf("size=%d digest=%s", size, digest)
}
