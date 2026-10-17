package js

import (
	"strconv"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
)

// Every snapshot stub must be overwritten in each realm by a Go-bound native
// or the bootstrap's own definition; a surviving no-op stub is a page-visible no-op global that the
// from-scratch realm (no snapshot, e.g. Linux) does not have.
// snapshotNoopStubSource is the exact text of the shared stub in
// snapshotStubsBootstrapTemplate.
const snapshotNoopStubSource = "function() { return undefined; }"

func TestSnapshotStubsAreReplacedInEveryRealm(t *testing.T) {
	doc, err := parser.ParseHTML(strings.NewReader("<html><body></body></html>"), "https://stubs.invalid/")
	if err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime()
	defer runtime.Close()
	// The invariant holds with the snapshot admitted (stubs replaced) and
	// without it (no stub ever installed, e.g. the darwin blob on Linux).
	t.Logf("snapshot state %s %s", runtime.snapshotState, runtime.snapshotReason)
	realm, err := runtime.NewContext(doc, ContextOpts{RequestContext: t.Context()})
	if err != nil {
		t.Fatal(err)
	}
	defer realm.Close()
	for _, name := range SnapshotNativeStubNames() {
		source, err := realm.EvalString(t.Context(), "typeof globalThis["+strconv.Quote(name)+"] === 'function' ? Function.prototype.toString.call(globalThis["+strconv.Quote(name)+"]) : 'missing'")
		if err != nil {
			t.Fatal(err)
		}
		if source == "missing" || source == snapshotNoopStubSource {
			t.Fatalf("snapshot stub %s survived as %s", name, source)
		}
	}
}
