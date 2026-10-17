package security

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// TrustUntrusted is the only trust value for page-derived content: spec
// L4202 allows a policy downgrade of scanning, never removal of the label.
const TrustUntrusted = "untrusted"

// ErrContentBlocked is returned when IDPI policy blocks page-derived content
// (blocklisted origin, or an injection phrase with BlockOnInjection).
var ErrContentBlocked = errors.New("idpi: page-derived content blocked by policy")

// ContentTrust labels page-derived output with its origin, producing source
// and IDPI verdict (spec L4202). TaintRefs carry the matched injection
// phrases so downstream consumers keep the taint.
type ContentTrust struct {
	Origin           string     `json:"origin"`
	Source           string     `json:"source"`
	Trust            string     `json:"trust"`
	Threat           IDPIThreat `json:"threat"`
	TaintRefs        []string   `json:"taintRefs,omitempty"`
	PolicyDowngraded bool       `json:"policyDowngraded,omitempty"`
}

// Label scans page-derived content from pageURL without fencing it and
// returns its trust label. Blocked content returns ErrContentBlocked with the
// label so callers can report why. A disabled checker still labels the
// content untrusted.
func (c *IDPIChecker) Label(pageURL, source string, content []byte) (ContentTrust, error) {
	origin, host := pageOrigin(pageURL)
	result := c.check(host, source, string(content), false)
	trust := ContentTrust{
		Origin:           origin,
		Source:           source,
		Trust:            TrustUntrusted,
		Threat:           result.Threat,
		TaintRefs:        result.TaintRefs,
		PolicyDowngraded: result.TrustLabel == "policy_downgraded",
	}
	if trust.Threat == IDPIThreatNone {
		trust.Threat = IDPIThreatUntrustedContent
	}
	if result.Blocked {
		return trust, fmt.Errorf("%w: %s", ErrContentBlocked, result.Reason)
	}
	return trust, nil
}

// Fence scans content like Label and returns it inside the spoof-proof
// untrusted-content fence, whatever WrapContent is configured to.
func (c *IDPIChecker) Fence(pageURL, source, content string) (string, ContentTrust, error) {
	trust, err := c.Label(pageURL, source, []byte(content))
	if err != nil {
		return "", trust, err
	}
	return wrapUntrusted(trust.Origin, source, content), trust, nil
}

// WithTrustMember adds trust as the top-level "trust" member of the JSON
// object raw, preserving the object's members and their order. Non-object
// JSON and objects that already own a "trust" member are rejected.
func WithTrustMember(raw []byte, trust ContentTrust) ([]byte, error) {
	return withMember(raw, "trust", trust)
}

// WithHARTrustMember adds trust as "_trust", the HAR custom-field form.
func WithHARTrustMember(raw []byte, trust ContentTrust) ([]byte, error) {
	return withMember(raw, "_trust", trust)
}

func withMember(raw []byte, name string, trust ContentTrust) ([]byte, error) {
	body := bytes.TrimSpace(raw)
	if len(body) < 2 || body[0] != '{' || body[len(body)-1] != '}' {
		return nil, errors.New("trust label: output is not a JSON object")
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(body, &members); err != nil {
		return nil, fmt.Errorf("trust label: %w", err)
	}
	if _, exists := members[name]; exists {
		return nil, fmt.Errorf("trust label: output already has a %q member", name)
	}
	encoded, err := json.Marshal(trust)
	if err != nil {
		return nil, fmt.Errorf("trust label: %w", err)
	}
	labeled := make([]byte, 0, len(body)+len(name)+len(encoded)+5)
	labeled = append(labeled, '{', '"')
	labeled = append(labeled, name...)
	labeled = append(labeled, '"', ':')
	labeled = append(labeled, encoded...)
	inner := bytes.TrimSpace(body[1 : len(body)-1])
	if len(inner) > 0 {
		labeled = append(labeled, ',')
		labeled = append(labeled, inner...)
	}
	return append(labeled, '}'), nil
}

// pageOrigin returns the scheme://host[:port] origin and lowercase hostname
// of pageURL; unparseable or opaque URLs keep their raw text as origin.
func pageOrigin(pageURL string) (origin, host string) {
	parsed, err := url.Parse(strings.TrimSpace(pageURL))
	if err != nil || parsed.Host == "" {
		return strings.TrimSpace(pageURL), ""
	}
	return parsed.Scheme + "://" + strings.ToLower(parsed.Host), strings.ToLower(parsed.Hostname())
}
