package main

import (
	"net/http"
	"testing"
	"time"
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
