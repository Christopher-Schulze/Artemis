package js

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestInstalledBindingInventoryObservesActualRealmAndSource(t *testing.T) {
	inventory, err := ObserveInstalledBindings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	expected, err := SourceHash()
	if err != nil || inventory.SourceHash != expected {
		t.Fatalf("inventory lost current source/test/native identity: %q %v", inventory.SourceHash, err)
	}
	bindings := make(map[string]InstalledBinding)
	for _, binding := range inventory.Bindings {
		bindings[binding.Path] = binding
	}
	for _, name := range []string{"$", "document", "fetch", "MutationObserver", "WebSocket", "crypto", "@element", "@input", "@form", "@iframe", "@canvas", "__fetch_abort", "__wrap"} {
		if _, ok := bindings[name]; !ok {
			t.Fatalf("actual installed binding omitted: %s", name)
		}
	}
	if bindings["window"].AliasOf != "$" || bindings["fetch"].Kind != "function" {
		t.Fatalf("actual alias/function shape changed: window=%+v fetch=%+v", bindings["window"], bindings["fetch"])
	}
	second, err := ObserveInstalledBindings(t.Context())
	if err != nil || !reflect.DeepEqual(inventory, second) {
		t.Fatalf("fresh realms yielded different descriptor graphs: %v", err)
	}
	t.Logf("observed %d actual descriptors, source=%s", len(inventory.Bindings), inventory.SourceHash)
}

func TestInstalledBindingInventoryRejectsCancellationAndBrokenGraphs(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ObserveInstalledBindings(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inventory admitted native work: %v", err)
	}
	for _, input := range []string{
		`[]`, `[{"path":"$","kind":"object"},{"path":"$","kind":"object"}]`,
		`[{"path":"$","kind":"object"},{"path":"a","kind":"object","alias_of":"missing"}]`,
		`[{"path":"$","kind":"object"},{"path":"a","kind":"object","alias_of":"b"},{"path":"b","kind":"object","alias_of":"a"}]`,
		`[{"path":"$","kind":"object"},{"path":"a","kind":"function","alias_of":"$"}]`,
		`[{"path":"$","kind":"object"},{"path":"a","kind":"invalid"}]`,
	} {
		if _, err := decodeBindingInventory(input); err == nil {
			t.Fatalf("broken graph accepted: %s", input)
		}
	}
}

func TestInstalledBindingReflectionDoesNotExecuteAccessors(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	if err := c.EvalDiscard(t.Context(), `globalThis.inventoryGetterCalls=0;Object.defineProperty(Object.prototype,'inventoryAccessor',{get(){inventoryGetterCalls++;throw Error('descriptor getter executed')},configurable:true});`); err != nil {
		t.Fatal(err)
	}
	encoded, err := c.EvalString(t.Context(), installedBindingObservation)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := decodeBindingInventory(encoded)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, binding := range inventory.Bindings {
		if strings.HasSuffix(binding.Path, ".inventoryAccessor") && binding.Kind == "accessor" && binding.HasGetter {
			found = true
		}
	}
	if calls, err := c.EvalString(t.Context(), "String(inventoryGetterCalls)"); err != nil || calls != "0" || !found {
		t.Fatalf("accessor omitted or executed: found=%v calls=%q %v", found, calls, err)
	}
}
