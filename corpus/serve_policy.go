package corpus

import (
	"encoding/json"
	"regexp"

	"github.com/Christopher-Schulze/Artemis/bridge"
)

func corpusActionPolicy(origin string) (*bridge.NavigationPolicy, error) {
	data, err := json.Marshal(bridge.PolicyFile{Version: bridge.PolicyFileVersion, Default: bridge.PolicyFileDeny,
		Allow: []string{"navigate", "evaluate", "wait", "extract", "tab_close"},
		Rules: []bridge.PolicyRule{{Action: bridge.PolicyFileAllow, Pattern: regexp.QuoteMeta(origin) + "/.*"}},
	})
	if err != nil {
		return nil, err
	}
	return bridge.NewStaticNavigationPolicy(data)
}
