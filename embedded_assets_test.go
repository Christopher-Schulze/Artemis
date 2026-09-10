package artemis_test

import (
	"encoding/hex"
	"testing"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/internal/wpt"
	"github.com/Christopher-Schulze/Artemis/js"
)

func TestEmbeddedAssetsAreCompleteAndHashed(t *testing.T) {
	manifest, merr := js.EmbeddedSnapshotManifest()
	if merr == nil && manifest.ToolchainRef != js.CurrentToolchainRef() {
		// Foreign toolchain: the asset surface must fail closed with the
		// manifest's compatibility error rather than serve stale assets.
		if _, err := artemis.EmbeddedAssets(); err == nil {
			t.Fatal("EmbeddedAssets served assets on a foreign toolchain")
		}
		return
	}
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
		digest, decodeErr := hex.DecodeString(asset.SHA256)
		if decodeErr != nil || len(digest) != 32 {
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
	snapshot := assets[0]
	if snapshot.SourceSetSHA256 == "" || snapshot.GeneratorRef == "" || snapshot.ToolchainRef == "" || snapshot.ActivationOwner == "" {
		t.Fatalf("snapshot provenance incomplete: %+v", snapshot)
	}
}
