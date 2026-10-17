package js

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/internal/sourcepin"
	"github.com/Christopher-Schulze/Artemis/network/cookiejar"
	"github.com/Christopher-Schulze/Artemis/webapi"
	v8 "github.com/tommie/v8go"
)

func TestJSBindingSourceHashIncludesEveryCurrentSource(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if _, err := conformanceSources.ReadFile(entry.Name()); err != nil {
			t.Fatalf("current JS source/test omitted from compiled conformance identity: %s %v", entry.Name(), err)
		}
	}
}

func TestCSSBindingSourceHashBindsActualInputsAndV8(t *testing.T) {
	entries, err := conformanceSources.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		actual, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		embedded, err := conformanceSources.ReadFile(entry.Name())
		if err != nil || !bytes.Equal(actual, embedded) {
			t.Fatalf("compiled CSS binding drift: %s %v", entry.Name(), err)
		}
	}
	native, err := sourcepin.Hash(v8.BindingSources(), v8.Version())
	if err != nil {
		t.Fatal(err)
	}
	cookies, err := cookiejar.SourceHash()
	if err != nil {
		t.Fatal(err)
	}
	dom, err := sourcepin.Hash(webapi.BindingSources(), "webapi")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := sourcepin.Hash(conformanceSources, v8.Version(), native, cookies, dom)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := SourceHash()
	if err != nil || actual != expected {
		t.Fatalf("CSS binding/V8 pin omitted inputs: %q %q %v", actual, expected, err)
	}
	foreign, err := sourcepin.Hash(conformanceSources, "different-v8", native, cookies, dom)
	if err != nil || foreign == actual {
		t.Fatalf("linked V8 identity did not affect the pin: %q %v", foreign, err)
	}
	foreign, err = sourcepin.Hash(conformanceSources, v8.Version(), "different-native-wrapper", cookies, dom)
	if err != nil || foreign == actual {
		t.Fatalf("native wrapper identity did not affect the pin: %q %v", foreign, err)
	}
	foreign, err = sourcepin.Hash(conformanceSources, v8.Version(), native, "different-cookie-owner", dom)
	if err != nil || foreign == actual {
		t.Fatalf("cookie owner identity did not affect the pin: %q %v", foreign, err)
	}
	foreign, err = sourcepin.Hash(conformanceSources, v8.Version(), native, cookies, "different-dom-owner")
	if err != nil || foreign == actual {
		t.Fatalf("DOM owner identity did not affect the pin: %q %v", foreign, err)
	}
}

func TestJSBindingSourceHashIncludesActualDOMOwner(t *testing.T) {
	entries, err := os.ReadDir("../webapi")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		actual, err := os.ReadFile("../webapi/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		embedded, err := webapi.BindingSources().ReadFile(entry.Name())
		if err != nil || !bytes.Equal(actual, embedded) {
			t.Fatalf("DOM owner source omitted or stale: %s %v", entry.Name(), err)
		}
	}
}

func TestJSBindingSourceHashIncludesActualNativeWrapper(t *testing.T) {
	entries, err := os.ReadDir("../third_party/v8go")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !(strings.HasSuffix(name, ".go") || name == "v8go.cc" || name == "v8go.h") {
			continue
		}
		actual, err := os.ReadFile("../third_party/v8go/" + name)
		if err != nil {
			t.Fatal(err)
		}
		embedded, err := v8.BindingSources().ReadFile(name)
		if err != nil || !bytes.Equal(actual, embedded) {
			t.Fatalf("native wrapper source omitted or stale: %s %v", name, err)
		}
	}
}
