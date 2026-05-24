package security

import (
	"strings"
	"unicode"
)

// HoneypotDecision describes whether an element should be skipped as a trap.
type HoneypotDecision struct {
	Skip    bool
	Reason  string
	FieldID string
}

// ClassifyHoneypot applies visibility and naming heuristics for bot traps (spec ss28 detection evasion).
func ClassifyHoneypot(attrs map[string]string) HoneypotDecision {
	if attrs == nil {
		return HoneypotDecision{}
	}
	id := strings.ToLower(strings.TrimSpace(attrs["id"]))
	class := strings.ToLower(strings.TrimSpace(attrs["class"]))
	style := strings.ToLower(strings.TrimSpace(attrs["style"]))
	name := strings.ToLower(strings.TrimSpace(attrs["name"]))
	combined := id + " " + class + " " + name
	for _, token := range []string{"honeypot", "hp-field", "bot-trap", "trap-field"} {
		if strings.Contains(combined, token) {
			return HoneypotDecision{Skip: true, Reason: "honeypot_name", FieldID: id}
		}
	}
	if strings.Contains(style, "display:none") || strings.Contains(style, "visibility:hidden") ||
		strings.Contains(style, "opacity:0") || strings.Contains(style, "height:0") {
		return HoneypotDecision{Skip: true, Reason: "invisible_style", FieldID: id}
	}
	if strings.Contains(class, "visually-hidden") || strings.Contains(class, "sr-only") {
		return HoneypotDecision{Skip: true, Reason: "screen_reader_only", FieldID: id}
	}
	return HoneypotDecision{}
}

// ContainsInvisibleChars reports zero-width or bidi override characters in operator input.
func ContainsInvisibleChars(s string) bool {
	for _, r := range s {
		if r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\u2060' ||
			r == '\u202a' || r == '\u202b' || r == '\u202c' || r == '\u202d' || r == '\u202e' {
			return true
		}
		if unicode.Is(unicode.Cf, r) {
			return true
		}
	}
	return false
}
