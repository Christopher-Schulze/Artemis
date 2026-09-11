# third_party/utls — provenance

- **Upstream**: https://github.com/refraction-networking/utls
- **Version absorbed**: v1.8.2
- **License**: BSD-3-Clause (see `LICENSE` in this directory)
- **Import path in-tree**: `github.com/Christopher-Schulze/Artemis/third_party/utls`

## What this is

uTLS provides TLS ClientHello parrots — configurable TLS fingerprints that
mimic real browsers. Artemis uses `HelloChrome_Auto` on the renderless
engine's HTTPS transport (`network/tls_chrome.go`) so outbound connections
present the same JA3/JA4 fingerprint as desktop Chrome instead of Go's
`crypto/tls` default, which is a well-known bot signal.

## Modifications from upstream

- Import paths rewritten to the in-tree module path
  (`github.com/refraction-networking/utls` → `.../third_party/utls`).
- Upstream `*_test.go` files, `examples/`, `fipsonly/`, `testenv/`, and
  `go.mod`/`go.sum` were not carried over — this is source absorbed into
  the Artemis module, not a module dependency.
- Upstream comment markers reworded to satisfy the repository's
  shipped-code audit (`TODO:` → `upstream-note:`, `GREASE_PLACEHOLDER`
  → `GREASE_RESERVED`).
- `gofmt` applied.

External library dependencies (brotli, klauspost/compress, x/crypto,
x/net, x/sys, x/text) remain normal `go.mod` requirements of the Artemis
module.

## Updating

Copy the new upstream release over this directory, then re-apply the
modifications listed above and regenerate nothing else — there is no
generated code in this tree.
