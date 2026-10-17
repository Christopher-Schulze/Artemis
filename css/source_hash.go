package css

import (
	"embed"
	"sync"

	"github.com/Christopher-Schulze/Artemis/internal/sourcepin"
)

//go:embed *.go
var conformanceSources embed.FS

var conformanceSourceHash = sync.OnceValues(func() (string, error) { return sourcepin.Hash(conformanceSources) })

// SourceHash pins the compiled CSS implementation and its failable tests.
func SourceHash() (string, error) { return conformanceSourceHash() }

// BindingSources supplies the existing compiled CSS owner and test inputs.
func BindingSources() embed.FS { return conformanceSources }
