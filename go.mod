module github.com/Christopher-Schulze/Artemis

go 1.26

require (
	github.com/andybalholm/cascadia v1.3.3
	github.com/bits-and-blooms/bloom/v3 v3.7.0
	github.com/coder/websocket v1.8.14
	github.com/google/uuid v1.6.0
	golang.org/x/crypto v0.50.0
	golang.org/x/net v0.53.0
	modernc.org/sqlite v1.34.5
	rogchap.com/v8go v0.9.0
)

require (
	github.com/bits-and-blooms/bitset v1.10.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/libc v1.55.3 // indirect
	modernc.org/mathutil v1.6.0 // indirect
	modernc.org/memory v1.8.0 // indirect
)

// Local fork with V8 snapshot support (TASK 042).
// SnapshotCreator + Isolate-from-snapshot bindings added beyond
// upstream v8go which does not expose this V8 facility.
replace rogchap.com/v8go => ./third_party/v8go
