package fixture

import (
	"fmt"
	"net/http"
	"sort"
)

// Kind is the category of a fixture scenario.
type Kind string

const (
	KindHTML          Kind = "html"
	KindCSS           Kind = "css"
	KindJS            Kind = "js"
	KindSPA           Kind = "spa"
	KindNavigation    Kind = "navigation"
	KindRedirect      Kind = "redirect"
	KindCookie        Kind = "cookie"
	KindStorage       Kind = "storage"
	KindForm          Kind = "form"
	KindFile          Kind = "file"
	KindFrame         Kind = "frame"
	KindShadowDOM     Kind = "shadow-dom"
	KindDialog        Kind = "dialog"
	KindCanvas        Kind = "canvas"
	KindWebSocket     Kind = "websocket"
	KindServiceWorker Kind = "service-worker"
	KindAuth          Kind = "auth"
	KindChallenge     Kind = "challenge"
	KindLarge         Kind = "large"
	KindMalformed     Kind = "malformed"
	KindSlow          Kind = "slow"
	KindCrash         Kind = "crash"
	KindUnknown       Kind = "unknown"
)

// Version is the fixture corpus version.
const Version = "1.0.0"

// Scenario is a deterministic fixture scenario served by a Server.
type Scenario struct {
	ID          string
	Path        string
	Kind        Kind
	Description string
	ContentType string
	Status      int
	HTML        string
	Handler     http.Handler `json:"-"`
	Expect      Expect
	RunScripts  bool
	AsyncFetch  bool
	WaitForIdle bool
	BodyBytes   int
	ScriptCount int
}

// Expect is the semantic outcome a consumer should observe after fetching
// the scenario. Empty fields are ignored.
type Expect struct {
	Status       int
	Title        string
	Contains     []string
	NotContains  []string
	Eval         string
	EvalContains string
	URL          string
}

// Validate returns an error if the Expect carries an invalid combination of fields.
func (e Expect) Validate() error {
	if e.Status != 0 && (e.Status < 100 || e.Status > 599) {
		return fmt.Errorf("Expect: invalid HTTP status %d", e.Status)
	}
	if e.Eval != "" && e.EvalContains == "" {
		return fmt.Errorf("Expect: Eval set without EvalContains")
	}
	return nil
}

// DefaultScenarios returns the complete local fixture corpus.
func DefaultScenarios() []Scenario {
	s := []Scenario{}
	s = append(s, htmlScenarios()...)
	s = append(s, cssScenarios()...)
	s = append(s, jsScenarios()...)
	s = append(s, spaScenarios()...)
	s = append(s, navigationScenarios()...)
	s = append(s, redirectScenarios()...)
	s = append(s, cookieScenarios()...)
	s = append(s, storageScenarios()...)
	s = append(s, formScenarios()...)
	s = append(s, fileScenarios()...)
	s = append(s, frameScenarios()...)
	s = append(s, shadowScenarios()...)
	s = append(s, dialogScenarios()...)
	s = append(s, canvasScenarios()...)
	s = append(s, websocketScenarios()...)
	s = append(s, serviceWorkerScenarios()...)
	s = append(s, authScenarios()...)
	s = append(s, challengeScenarios()...)
	s = append(s, largeScenarios()...)
	s = append(s, malformedScenarios()...)
	s = append(s, slowScenarios()...)
	s = append(s, crashScenarios()...)
	return s
}

// ScenariosByKind returns the registered scenarios of the given kind.
func ScenariosByKind(scenarios []Scenario, kind Kind) []Scenario {
	var out []Scenario
	for _, sc := range scenarios {
		if sc.Kind == kind {
			out = append(out, sc)
		}
	}
	return out
}

// ScenarioByID returns the first scenario with the given ID, or nil.
func ScenarioByID(id string) *Scenario {
	for _, sc := range DefaultScenarios() {
		if sc.ID == id {
			return &sc
		}
	}
	return nil
}

// ScenariosByPath returns the scenarios sorted by path.
func ScenariosByPath(scenarios []Scenario) []Scenario {
	out := append([]Scenario(nil), scenarios...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
