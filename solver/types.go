package solver

// ChallengeType classifies detected challenges (spec ss28.6.1.2).
type ChallengeType string

const (
	TypeNone       ChallengeType = "none"
	TypeCloudflare ChallengeType = "cloudflare"
	TypeRecaptcha  ChallengeType = "recaptcha"
	TypeHCaptcha   ChallengeType = "hcaptcha"
	TypeGeneric    ChallengeType = "generic"
)

// ChallengeInfo is the detector output.
type ChallengeInfo struct {
	Type       ChallengeType
	Confidence float64
	ElementRef string
	PageTitle  string
	Domain     string // host of the page where the challenge was detected (for metrics)
}

// PageSignals is DOM/title input for detection (no live browser required in unit tests).
type PageSignals struct {
	Title string
	HTML  string
	URL   string
}
