package main

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/diagnostics"
	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func TestParseHeaderFlagsAcceptsEqualsAndColon(t *testing.T) {
	got, err := parseHeaderFlags([]string{"X-Test=one", "Accept: text/html"})
	if err != nil {
		t.Fatalf("parseHeaderFlags: %v", err)
	}
	if got.Get("X-Test") != "one" {
		t.Fatalf("X-Test = %q, want one", got.Get("X-Test"))
	}
	if got.Get("Accept") != "text/html" {
		t.Fatalf("Accept = %q, want text/html", got.Get("Accept"))
	}
}

func TestParseHeaderFlagsRejectsInvalidInput(t *testing.T) {
	if _, err := parseHeaderFlags([]string{"broken"}); err == nil {
		t.Fatal("expected invalid header error")
	}
	if _, err := parseHeaderFlags([]string{" =value"}); err == nil {
		t.Fatal("expected empty header key error")
	}
}

func TestParseHeaderFlagsEmptyIsNil(t *testing.T) {
	got, err := parseHeaderFlags(nil)
	if err != nil {
		t.Fatalf("parseHeaderFlags nil: %v", err)
	}
	if got != nil {
		t.Fatalf("got %#v, want nil", http.Header(got))
	}
}

func TestParseDurationUsesDefaultAndParsesValues(t *testing.T) {
	def := 5 * time.Second
	got, err := parseDuration("", def)
	if err != nil {
		t.Fatalf("parseDuration default: %v", err)
	}
	if got != def {
		t.Fatalf("default duration = %s, want %s", got, def)
	}

	got, err = parseDuration("250ms", def)
	if err != nil {
		t.Fatalf("parseDuration value: %v", err)
	}
	if got != 250*time.Millisecond {
		t.Fatalf("duration = %s, want 250ms", got)
	}
}

func TestParseDurationRejectsInvalidValue(t *testing.T) {
	if _, err := parseDuration("soon", time.Second); err == nil {
		t.Fatal("expected invalid duration error")
	}
	if _, err := parseDuration("0s", time.Second); err == nil {
		t.Fatal("expected zero duration error")
	}
	if _, err := parseDuration("-1s", time.Second); err == nil {
		t.Fatal("expected negative duration error")
	}
}

func TestStringSliceFlagAppendsAndFormatsValues(t *testing.T) {
	var values stringSliceFlag
	if err := values.Set("a"); err != nil {
		t.Fatalf("set a: %v", err)
	}
	if err := values.Set("b"); err != nil {
		t.Fatalf("set b: %v", err)
	}
	if got := values.String(); got != "a,b" {
		t.Fatalf("String() = %q, want a,b", got)
	}
}

func TestProcessDiagnosticsCorrelatePolicyAndResourceRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artemis.jsonl")
	t.Setenv("ARTEMIS_DIAGNOSTICS_FILE", path)
	store, policySink, resourceSink, err := newProcessDiagnostics("chromium", "act-session")
	if err != nil {
		t.Fatal(err)
	}
	if policyErr := policySink(network.Decision{
		Action: network.DecisionAllow, Kind: network.TargetSocket, Scheme: "tcp",
		Host: "example.test", Port: 443, Reason: "policy_match", SessionID: "proxy-internal",
	}); policyErr != nil {
		t.Fatal(policyErr)
	}
	if resourceErr := resourceSink(browserprocess.ResourceUsage{CPUPercent: 1, MemoryBytes: 2, ProfileDiskBytes: 3}); resourceErr != nil {
		t.Fatal(resourceErr)
	}
	records, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	want := diagnostics.HashSession("act-session")
	if len(records) != 2 || records[0].Policy == nil || records[1].Resource == nil || records[0].Policy.SessionRef != want || records[1].Resource.SessionRef != want {
		t.Fatalf("records=%+v want session_ref=%q", records, want)
	}
}
