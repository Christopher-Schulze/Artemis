package v8go

import "embed"

//go:embed *.go *.cc *.h
var bindingSources embed.FS

// BindingSources supplies immutable wrapper and test inputs for the embedding
// application's conformance digest. It does not identify the native archive.
func BindingSources() embed.FS { return bindingSources }
