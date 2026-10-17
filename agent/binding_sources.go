package agent

import "embed"

//go:embed *.go
var bindingSources embed.FS

// BindingSources supplies immutable extraction-owner and test inputs.
func BindingSources() embed.FS { return bindingSources }
