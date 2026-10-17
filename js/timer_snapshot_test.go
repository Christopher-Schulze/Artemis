package js

import (
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
)

func TestTimersBindNativeOwnerAfterActualSourceSnapshot(t *testing.T) {
	// The runtime admits the committed snapshot only when its manifest binds
	// the actual bootstrap sources, so an active snapshot here is the actual
	// source snapshot.
	rt := NewRuntime()
	if _, matching := currentToolchainMatches(t); matching && !rt.hasSnapshot {
		t.Fatalf("snapshot did not activate: %s", rt.snapshotReason)
	}
	defer rt.Close()
	doc, err := parser.ParseHTML(strings.NewReader("<html><body></body></html>"), "https://timer.test/")
	if err != nil {
		t.Fatal(err)
	}
	c, err := rt.NewContext(doc, ContextOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	value, err := c.Eval(t.Context(), `
		globalThis.fired = 0;
		const cancelled = setTimeout(() => { fired += 100; }, 0);
		clearInterval(cancelled);
		setTimeout(function(n) { fired += this === globalThis ? n : 1000; }, 0, 7);
	`)
	if err != nil {
		t.Fatal(err)
	}
	value, err = c.Eval(t.Context(), "fired")
	if err != nil || value.String() != "7" || len(c.timers.pending) != 0 {
		t.Fatalf("snapshot did not bind real timer callbacks: value=%v pending=%d err=%v", value, len(c.timers.pending), err)
	}
}
