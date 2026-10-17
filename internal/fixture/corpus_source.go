package fixture

import (
	"embed"
	"sync"

	"github.com/Christopher-Schulze/Artemis/css"
	"github.com/Christopher-Schulze/Artemis/internal/sourcepin"
	"github.com/Christopher-Schulze/Artemis/js"
)

// Sources are embedded only for an automatic implementation pin. Source bytes
// are never serialized into a fixture asset or served by the fixture server.
//
//go:embed scenario.go server.go validation.go corpus.go corpus_source.go corpus_server.go corpus_replay.go corpus_engine.go corpus_native.go corpus_hybrid.go scenarios_advanced.go scenarios_css.go scenarios_forms.go scenarios_frames.go scenarios_html.go scenarios_javascript.go scenarios_network.go scenarios_spa.go
var corpusSources embed.FS

var corpusSourceHash = sync.OnceValues(func() (string, error) {
	cssHash, err := css.SourceHash()
	if err != nil {
		return "", err
	}
	jsHash, err := js.SourceHash()
	if err != nil {
		return "", err
	}
	return sourcepin.Hash(corpusSources, cssHash, jsHash)
})
