package stealth

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Environment variable names for operator opt-in stealth policy.
const (
	EnvStealthLevel      = "ARTEMIS_STEALTH"
	EnvStealthPurpose    = "ARTEMIS_STEALTH_PURPOSE"
	EnvStealthLegalBasis = "ARTEMIS_STEALTH_LEGAL_BASIS"
	EnvStealthDomains    = "ARTEMIS_STEALTH_DOMAINS"
)

// PolicyFromEnv reads the operator stealth policy from the process
// environment. Setting ARTEMIS_STEALTH to "stealth" or "paranoid" opts the
// process in; the legal gate still requires ARTEMIS_STEALTH_PURPOSE and
// ARTEMIS_STEALTH_LEGAL_BASIS so the acknowledgement is explicit rather than
// implicit. ARTEMIS_STEALTH_DOMAINS optionally restricts stealth to a
// comma-separated host allow-list.
//
// The second return value reports whether stealth was requested at all.
// A requested-but-incomplete policy is an error, not a silent downgrade:
// silently continuing unstealthed is how operators get CAPTCHA walls.
func PolicyFromEnv() (StealthPolicy, bool, error) {
	level := StealthLevel(strings.ToLower(strings.TrimSpace(os.Getenv(EnvStealthLevel))))
	if level == "" || level == "off" || level == StealthDefault {
		return StealthPolicy{PublicDefault: StealthDefault}, false, nil
	}
	if level != StealthStealth && level != StealthParanoid {
		return StealthPolicy{}, true, fmt.Errorf("stealth env: %s must be stealth or paranoid, got %q", EnvStealthLevel, level)
	}
	purpose := strings.TrimSpace(os.Getenv(EnvStealthPurpose))
	basis := strings.TrimSpace(os.Getenv(EnvStealthLegalBasis))
	if purpose == "" || basis == "" {
		return StealthPolicy{}, true, errors.New("stealth env: " + EnvStealthPurpose + " and " + EnvStealthLegalBasis + " are required when " + EnvStealthLevel + " is set")
	}
	var domains []string
	for _, d := range strings.Split(os.Getenv(EnvStealthDomains), ",") {
		if d = strings.TrimSpace(d); d != "" {
			domains = append(domains, d)
		}
	}
	return StealthPolicy{
		PublicDefault: level,
		Ack: StealthAck{
			AcknowledgedAt: time.Now(),
			LegalBasis:     basis,
			Purpose:        purpose,
			DomainAllow:    domains,
		},
	}, true, nil
}
