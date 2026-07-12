package security

import "testing"

func TestSecretScrubberRedactsAndWipes(t *testing.T) {
	scrubber := NewSecretScrubber("")
	if err := scrubber.Add([]byte("password-value")); err != nil {
		t.Fatal(err)
	}
	if got := scrubber.Redact("user=password-value"); got != "user=[REDACTED]" {
		t.Fatalf("redaction=%q", got)
	}
	if !scrubber.Contains("password-value") {
		t.Fatal("secret was not detected")
	}
	scrubber.Close()
	if got := scrubber.Redact("password-value"); got != "password-value" {
		t.Fatalf("closed scrubber retained secret=%q", got)
	}
}
