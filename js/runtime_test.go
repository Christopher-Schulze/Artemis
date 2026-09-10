package js

import (
	"context"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/parser"
)

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

func TestRuntimeReportsActiveSnapshot(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	status := rt.SnapshotStatus()
	manifest, merr := EmbeddedSnapshotManifest()
	if merr == nil && manifest.ToolchainRef != CurrentToolchainRef() {
		// Foreign toolchain: the committed blob must NOT activate.
		if status.State == SnapshotActive {
			t.Fatalf("snapshot activated on foreign toolchain: %q", manifest.ToolchainRef)
		}
		return
	}
	if status.State != SnapshotActive {
		t.Fatalf("snapshot state=%q, reason=%q", status.State, status.Reason)
	}
	if status.Reason != "" {
		t.Fatalf("active snapshot reason=%q", status.Reason)
	}
}

func TestRuntimeRejectsCorruptSnapshotWithoutActivation(t *testing.T) {
	original := snapshotBlob
	defer func() { snapshotBlob = original }()
	corrupt := append([]byte(nil), snapshotBlob...)
	corrupt[len(corrupt)-1] ^= 0x01
	snapshotBlob = corrupt

	rt := NewRuntime()
	defer rt.Close()
	status := rt.SnapshotStatus()
	if status.State != SnapshotUnavailable {
		t.Fatalf("snapshot state=%q, reason=%q", status.State, status.Reason)
	}
	if status.Reason == "" {
		t.Fatal("corrupt snapshot was rejected without an observable reason")
	}
}

func TestSnapshotAndColdRuntimeParity(t *testing.T) {
	snapshot := runSnapshotParityProbe(t, true)
	cold := runSnapshotParityProbe(t, false)
	if snapshot != cold {
		t.Fatalf("snapshot/cold parity mismatch: snapshot=%q cold=%q", snapshot, cold)
	}
	snapshotPool := runSnapshotPoolProbe(t, true)
	coldPool := runSnapshotPoolProbe(t, false)
	if snapshotPool != coldPool {
		t.Fatalf("snapshot/cold pool parity mismatch: snapshot=%q cold=%q", snapshotPool, coldPool)
	}
}

func runSnapshotParityProbe(t *testing.T, useSnapshot bool) string {
	t.Helper()
	originalBlob := snapshotBlob
	defer func() { snapshotBlob = originalBlob }()
	if !useSnapshot {
		snapshotBlob = nil
	}
	doc, err := parser.ParseHTML(strings.NewReader(`<html><body><button id="b">x</button><p id="p">hello</p></body></html>`), "https://example.test/")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rt := NewRuntime()
	defer rt.Close()
	ctx, err := rt.NewContext(doc, ContextOpts{
		Fetch: func(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
			return &FetchResponse{Status: 200, StatusText: "OK", Body: []byte("fetched"), URL: req.URL}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	defer ctx.Close()
	if _, err := ctx.Eval(context.Background(), `
		var eventCount = 0;
		document.getElementById('b').addEventListener('click', () => { eventCount++; });
		document.getElementById('b').dispatchEvent(new Event('click'));
		var fetched = '';
		(async () => { fetched = await (await fetch('https://example.test/data')).text(); })();
		var digest = '';
		(async () => {
			const bytes = await crypto.subtle.digest('SHA-256', new TextEncoder().encode('hello'));
			digest = Array.from(new Uint8Array(bytes)).map(v => v.toString(16).padStart(2, '0')).join('');
		})();
	`); err != nil {
		t.Fatalf("probe eval: %v", err)
	}
	values := make([]string, 0, 4)
	for _, expression := range []string{`document.getElementById('p').textContent`, `String(eventCount)`, `fetched`, `digest`} {
		value, err := ctx.Eval(context.Background(), expression)
		if err != nil {
			t.Fatalf("probe %s: %v", expression, err)
		}
		values = append(values, value.String())
	}
	return strings.Join(values, "|")
}

func runSnapshotPoolProbe(t *testing.T, useSnapshot bool) string {
	t.Helper()
	originalBlob := snapshotBlob
	defer func() { snapshotBlob = originalBlob }()
	if !useSnapshot {
		snapshotBlob = nil
	}
	doc, err := parser.ParseHTML(strings.NewReader(`<html></html>`), "https://example.test/")
	if err != nil {
		t.Fatalf("parse pool document: %v", err)
	}
	rt := NewRuntimeWithPool(1)
	first, err := rt.NewContext(doc, ContextOpts{})
	if err != nil {
		rt.Close()
		t.Fatalf("first pooled context: %v", err)
	}
	if _, evalErr := first.Eval(context.Background(), `globalThis.__parity_pool_probe = 'set'`); evalErr != nil {
		first.Close()
		rt.Close()
		t.Fatalf("first pooled eval: %v", evalErr)
	}
	first.Close()
	second, err := rt.NewContext(doc, ContextOpts{})
	if err != nil {
		rt.Close()
		t.Fatalf("second pooled context: %v", err)
	}
	value, err := second.Eval(context.Background(), `typeof globalThis.__parity_pool_probe`)
	if err != nil {
		second.Close()
		rt.Close()
		t.Fatalf("second pooled eval: %v", err)
	}
	result := value.String()
	second.Close()
	rt.Close()
	return result
}
