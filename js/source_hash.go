package js

import (
	"embed"
	"sync"

	"github.com/Christopher-Schulze/Artemis/internal/sourcepin"
	"github.com/Christopher-Schulze/Artemis/network/cookiejar"
	"github.com/Christopher-Schulze/Artemis/webapi"
	v8 "github.com/tommie/v8go"
)

//go:embed *.go
var conformanceSources embed.FS

// BindingSources supplies immutable JS-owner/test inputs for conformance.
func BindingSources() embed.FS { return conformanceSources }

var conformanceSourceHash = sync.OnceValues(func() (string, error) {
	native, err := sourcepin.Hash(v8.BindingSources(), v8.Version())
	if err != nil {
		return "", err
	}
	cookies, err := cookiejar.SourceHash()
	if err != nil {
		return "", err
	}
	dom, err := sourcepin.Hash(webapi.BindingSources(), "webapi")
	if err != nil {
		return "", err
	}
	return sourcepin.Hash(conformanceSources, v8.Version(), native, cookies, dom)
})

// SourceHash pins JS bindings/bootstraps/tests, native wrapper sources/tests and
// the linked V8 version and owning cookie/DOM implementations/tests.
// It does not claim a native Chromium or complete binary identity.
func SourceHash() (string, error) { return conformanceSourceHash() }
