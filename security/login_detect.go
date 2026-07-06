package security

import "strings"

// DetectLoginFlow reports common login-wall phrases in page text.
func DetectLoginFlow(text string) bool {
	lower := strings.ToLower(text)
	for _, phrase := range []string{"sign in", "log in", "login", "authenticate"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}
