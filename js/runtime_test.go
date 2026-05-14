package js

import "testing"

func TestRuntimeCloseIdempotent(t *testing.T) {
	rt := NewRuntime()
	rt.Close()
	rt.Close()
}

func TestRuntimeIsolateNonNil(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	if rt.Isolate() == nil {
		t.Error("Isolate() == nil")
	}
}
