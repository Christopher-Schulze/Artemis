package artemis_test

import (
	"encoding/hex"
	"testing"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/internal/wpt"
)

func TestEmbeddedAssetsAreCompleteAndHashed(t *testing.T) {
	assets, err := artemis.EmbeddedAssets()
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 3 {
		t.Fatalf("asset count=%d", len(assets))
	}
	for _, asset := range assets {
		if asset.Ref == "" || asset.SourcePath == "" || asset.MIME == "" || asset.Size <= 0 {
			t.Fatalf("incomplete asset=%+v", asset)
		}
		digest, err := hex.DecodeString(asset.SHA256)
		if err != nil || len(digest) != 32 {
			t.Fatalf("invalid digest for %s: %q", asset.Ref, asset.SHA256)
		}
	}

	wptSize, wptDigest, err := wpt.EmbeddedAssetManifest()
	if err != nil {
		t.Fatal(err)
	}
	wptAsset := assets[2]
	if wptAsset.Size != wptSize || wptAsset.SHA256 != wptDigest {
		t.Fatalf("WPT manifest drift: release=%d/%s embedded=%d/%s", wptAsset.Size, wptAsset.SHA256, wptSize, wptDigest)
	}
}
