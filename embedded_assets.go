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
// coupling the reusable browser module to Artemis release types.
type EmbeddedAsset struct {
	Ref             string
	SourcePath      string
	MIME            string
	Size            int64
	SHA256          string
	SourceSetSHA256 string
	GeneratorRef    string
	ToolchainRef    string
	ActivationOwner string
}

// EmbeddedAssets returns the complete Artemis go:embed inventory used by the
// production runtime.
func EmbeddedAssets() ([]EmbeddedAsset, error) {
	snapshotManifest, err := js.CurrentSnapshotManifest()
	if err != nil {
		return nil, err
	}
	return []EmbeddedAsset{
		{
			Ref:             "artemis-v8-startup-snapshot",
			SourcePath:      snapshotManifest.SourcePath,
			MIME:            snapshotManifest.MIME,
			Size:            snapshotManifest.Size,
			SHA256:          snapshotManifest.SHA256,
			SourceSetSHA256: snapshotManifest.SourceSetSHA256,
			GeneratorRef:    snapshotManifest.GeneratorRef,
			ToolchainRef:    snapshotManifest.ToolchainRef,
			ActivationOwner: snapshotManifest.ActivationOwner,
		},
		{Ref: "artemis-stealth-bundle", SourcePath: "codebase/backend/artemis/stealth/stealth_bundle.js", MIME: "text/javascript", Size: int64(stealth.BundledScriptSize()), SHA256: stealth.BundledScriptHash()},
		{Ref: "artemis-wpt-subset", SourcePath: "codebase/backend/artemis/internal/wpt/testdata/wpt", MIME: "application/vnd.artemis.asset-tree", Size: embeddedWPTAssetSize, SHA256: embeddedWPTAssetSHA256},
	}, nil
}
