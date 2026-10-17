package cookiejar

import (
	"embed"
	"runtime"
	"sync"

	"github.com/Christopher-Schulze/Artemis/internal/sourcepin"
)

//go:embed *.go LICENSE
var sourceInputs embed.FS

var sourceHash = sync.OnceValues(func() (string, error) {
	return sourcepin.Hash(sourceInputs, runtime.Version())
})

// SourceHash pins the single cookie owner, its tests, license and Go toolchain.
func SourceHash() (string, error) { return sourceHash() }
