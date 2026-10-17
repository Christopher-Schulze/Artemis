//go:build cgo && darwin

package v8go

// The C++ binding is precompiled on darwin with the Clang and libc++ headers
// V8 requires, so builds need no special CC or CGO_CXXFLAGS. Rebuild the
// archives with Artemis' cmd/artemis-v8go-binding after changing any .cc
// or .h file; TestBindingArchiveMatchesSources enforces it.

// #cgo darwin,arm64 LDFLAGS: -L${SRCDIR}/lib/darwin_arm64 -lv8go_artemis
// #cgo darwin,amd64 LDFLAGS: -L${SRCDIR}/lib/darwin_amd64 -lv8go_artemis
import "C"
