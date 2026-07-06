package prompts

import (
	"fmt"
	"testing"
)

// =============================================================================
// SP-artemis-prompts-SEC (prompts.go, security_privacy)
// Claim: GetPrompt and GetPromptTemplate deny unknown prompt types
// =============================================================================

func TestWFArtemisPrompts_DenyUnknownPromptType(t *testing.T) {
	// Security: prompt lookup must deny unknown prompt types to prevent
	// injection of arbitrary prompt content via invalid type indices.

	unknownTypes := []PromptType{
		PromptType(-1),
		PromptType(100),
		PromptType(255),
		PromptType(42),
	}
	blocked := 0
	for _, pt := range unknownTypes {
		tmpl, err := GetPrompt(pt)
		if err == nil {
			t.Fatalf("GetPrompt(%d): expected error, got tmpl=%q", pt, tmpl)
		}
		if tmpl != "" {
			t.Fatalf("GetPrompt(%d): expected empty string, got %q", pt, tmpl)
		}
		blocked++
	}
	denyRate := float64(blocked) / float64(len(unknownTypes))
	fmt.Printf("deny_rate=%.1f blocked=1\n", denyRate)
	if denyRate != 1.0 {
		t.Fatalf("expected deny_rate=1.0 (all unknown types denied), got %.1f", denyRate)
	}

	// GetPromptTemplate must also deny unknown types.
	for _, pt := range unknownTypes {
		tmpl, err := GetPromptTemplate(pt)
		if err == nil {
			t.Fatalf("GetPromptTemplate(%d): expected error, got tmpl=%+v", pt, tmpl)
		}
		if tmpl != nil {
			t.Fatalf("GetPromptTemplate(%d): expected nil, got %+v", pt, tmpl)
		}
		blocked++
	}

	// Baseline: all 9 known types succeed (positive control).
	all := AllPromptTypes()
	if len(all) != 9 {
		t.Fatalf("expected 9 prompt types, got %d", len(all))
	}
	for _, pt := range all {
		tmpl, err := GetPrompt(pt)
		if err != nil {
			t.Fatalf("GetPrompt(%d): known type must succeed, got err=%v", pt, err)
		}
		if tmpl == "" {
			t.Fatalf("GetPrompt(%d): known type must return non-empty template", pt)
		}
	}

	// Baseline: String() returns "unknown" for invalid types.
	if PromptType(999).String() != "unknown" {
		t.Fatalf("expected 'unknown' for invalid type, got %q", PromptType(999).String())
	}
}
