package solver

import "time"

// ChallengeType classifies detected challenges (spec ss28.6.1.2).
type ChallengeType string

const (
	TypeNone       ChallengeType = "none"
	TypeCloudflare ChallengeType = "cloudflare"
	TypeRecaptcha  ChallengeType = "recaptcha"
	TypeHCaptcha   ChallengeType = "hcaptcha"
	TypeGeneric    ChallengeType = "generic"
)

type ChallengeSignalSource string

const (
	SignalDOM      ChallengeSignalSource = "dom"
	SignalNetwork  ChallengeSignalSource = "network"
	SignalTitle    ChallengeSignalSource = "title"
	SignalResponse ChallengeSignalSource = "response"
	SignalVisual   ChallengeSignalSource = "visual"
)

type ChallengeSignal struct {
	Source  ChallengeSignalSource `json:"source"`
	Marker  string                `json:"marker"`
	Weight  int                   `json:"weight"`
	Present bool                  `json:"present"`
}

// ChallengeInfo is the detector output.
type ChallengeInfo struct {
	Type       ChallengeType     `json:"type"`
	Confidence float64           `json:"confidence"`
	ElementRef string            `json:"element_ref,omitempty"`
	PageTitle  string            `json:"page_title,omitempty"`
	Domain     string            `json:"domain,omitempty"`
	Signals    []ChallengeSignal `json:"signals,omitempty"`
}

// PageSignals is DOM/title input for detection (no live browser required in unit tests).
type PageSignals struct {
	Title           string
	HTML            string
	URL             string
	StatusCode      int
	NetworkURLs     []string
	ResponseHeaders map[string]string
	VisualText      string
	ElementMarkers  []string
	ObservedAt      time.Time
}
