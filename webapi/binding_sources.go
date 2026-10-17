package webapi

import "embed"

//go:embed *.go
var bindingSources embed.FS

// BindingSources supplies immutable DOM-owner/test inputs for conformance.
func BindingSources() embed.FS { return bindingSources }
