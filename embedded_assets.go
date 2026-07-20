package artemis

import (
	"github.com/Christopher-Schulze/Artemis/js"
	"github.com/Christopher-Schulze/Artemis/stealth"
)

const (
	embeddedWPTAssetSize   int64 = 202545
	embeddedWPTAssetSHA256       = "9c0a6089d380f6153137b0b0951fd89ca2dc814ef02e32d8091ae395112e3ba5"
)

// EmbeddedAsset describes an Artemis-owned immutable binary asset without
// coupling the reusable browser module to Omnimus release types.
type EmbeddedAsset struct {
	Ref        string
	SourcePath string
	MIME       string
	Size       int64
	SHA256     string
}

// EmbeddedAssets returns the complete Artemis go:embed inventory used by the
// production runtime.
func EmbeddedAssets() ([]EmbeddedAsset, error) {
	return []EmbeddedAsset{
		{Ref: "artemis-v8-startup-snapshot", SourcePath: "codebase/backend/artemis/js/snapshot.bin", MIME: "application/octet-stream", Size: js.SnapshotAssetSize(), SHA256: js.SnapshotAssetSHA256()},
		{Ref: "artemis-stealth-bundle", SourcePath: "codebase/backend/artemis/stealth/stealth_bundle.js", MIME: "text/javascript", Size: int64(stealth.BundledScriptSize()), SHA256: stealth.BundledScriptHash()},
		{Ref: "artemis-wpt-subset", SourcePath: "codebase/backend/artemis/internal/wpt/testdata/wpt", MIME: "application/vnd.omnimus.asset-tree", Size: embeddedWPTAssetSize, SHA256: embeddedWPTAssetSHA256},
	}, nil
}
