module artemis

go 1.26

require (
	github.com/andybalholm/cascadia v1.3.3
	github.com/coder/websocket v1.8.14
	golang.org/x/crypto v0.50.0
	golang.org/x/net v0.53.0
	rogchap.com/v8go v0.9.0
)

// Local fork with V8 snapshot support (TASK 042).
// SnapshotCreator + Isolate-from-snapshot bindings added beyond
// upstream v8go which does not expose this V8 facility.
replace rogchap.com/v8go => ./third_party/v8go
