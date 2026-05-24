package security

// ConsentAction is the default handling for cookie/consent prompts.
type ConsentAction string

const (
	ConsentDeny    ConsentAction = "deny"
	ConsentAccept  ConsentAction = "accept"
)

func DefaultConsentAction() ConsentAction {
	return ConsentDeny
}
