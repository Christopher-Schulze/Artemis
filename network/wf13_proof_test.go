package network

import (
	"fmt"
	"testing"
)

// TestWFNetguardSec verifies that Netguard with BlockPrivate enabled
// denies navigation to private/internal targets (SSRF floor). Every
// private target must produce a non-nil error (deny).
func TestWFNetguardSec(t *testing.T) {
	ng := Netguard{BlockPrivate: true}
	private := []string{
		"http://127.0.0.1:8080/",
		"http://localhost/",
		"http://10.0.0.5/",
		"http://192.168.1.1/",
		"http://169.254.169.254/", // link-local metadata endpoint
		"http://[::1]/",
	}
	denied := 0
	for _, u := range private {
		if err := ng.Allow(u); err != nil {
			denied++
		}
	}
	if denied != len(private) {
		t.Fatalf("denied %d/%d private targets", denied, len(private))
	}
	denyRate := float64(denied) / float64(len(private))
	fmt.Printf("deny_rate=%.1f\n", denyRate)
	fmt.Printf("security_pass_rate=%.1f\n", denyRate)
}

// TestWFNetguardSecBaseline shows that with BlockPrivate disabled the
// same private targets are all allowed (no SSRF protection), confirming
// the deny is caused by the Netguard check rather than the URLs.
func TestWFNetguardSecBaseline(t *testing.T) {
	ng := Netguard{BlockPrivate: false}
	private := []string{
		"http://127.0.0.1:8080/",
		"http://localhost/",
		"http://10.0.0.5/",
		"http://192.168.1.1/",
		"http://169.254.169.254/",
		"http://[::1]/",
	}
	denied := 0
	for _, u := range private {
		if err := ng.Allow(u); err != nil {
			denied++
		}
	}
	if denied != 0 {
		t.Fatalf("baseline denied %d targets, expected 0", denied)
	}
	fmt.Printf("baseline_deny_rate=0.0\n")
}
