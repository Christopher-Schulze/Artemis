module github.com/Christopher-Schulze/Artemis

go 1.27

require (
	github.com/CycloneDX/cyclonedx-go v0.11.0
	github.com/andybalholm/brotli v1.0.6
	github.com/andybalholm/cascadia v1.3.3
	github.com/bits-and-blooms/bloom/v3 v3.7.0
	github.com/coder/websocket v1.8.14
	github.com/cristalhq/base64 v0.1.2
	github.com/disintegration/imaging v1.6.2
	github.com/google/uuid v1.6.0
	github.com/klauspost/compress v1.17.4
	github.com/modelcontextprotocol/go-sdk v1.7.0
	github.com/pjbgf/sha1cd v0.6.0
	github.com/remitly-oss/httpsig-go v1.2.2
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
	github.com/tdewolff/parse/v2 v2.8.16
	github.com/tommie/v8go v0.39.0
	golang.org/x/crypto v0.51.0
	golang.org/x/image v0.47.0
	golang.org/x/net v0.55.0
	golang.org/x/sys v0.49.0
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.34.5
)

require (
	github.com/bits-and-blooms/bitset v1.10.0 // indirect
	github.com/dunglas/httpsfv v1.0.2 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/tommie/v8go/deps/android_amd64 v0.0.0-20261016055159-bef8086aea9f // indirect
	github.com/tommie/v8go/deps/android_arm64 v0.0.0-20261016055159-bef8086aea9f // indirect
	github.com/tommie/v8go/deps/darwin_amd64 v0.0.0-20261016055159-bef8086aea9f // indirect
	github.com/tommie/v8go/deps/darwin_arm64 v0.0.0-20261016055159-bef8086aea9f // indirect
	github.com/tommie/v8go/deps/linux_amd64 v0.0.0-20261016055159-bef8086aea9f // indirect
	github.com/tommie/v8go/deps/linux_arm64 v0.0.0-20261016055159-bef8086aea9f // indirect
	github.com/tommie/v8go/deps/windows_amd64 v0.0.0-20261016055159-bef8086aea9f // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	modernc.org/libc v1.55.3 // indirect
	modernc.org/mathutil v1.6.0 // indirect
	modernc.org/memory v1.8.0 // indirect
)

// Vendored tommie/v8go (maintained upstream) with the Artemis patches in
// third_party/v8go/ARTEMIS_PATCHES.md; V8 libraries come from the upstream
// github.com/tommie/v8go/deps/<os_arch> modules.
replace github.com/tommie/v8go => ./third_party/v8go
