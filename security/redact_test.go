package security

import (
	"regexp"
	"strings"
	"sync"
	"testing"
)

func mustRedactor(t *testing.T, cfg RedactionConfig) *Redactor {
	t.Helper()
	return NewRedactor(cfg)
}

func TestRedactAPIKey(t *testing.T) {
	r := mustRedactor(t, DefaultRedactionConfig())
	in := "use api_key=sk_test_4eC39HqLyjWDarjtT1zdp7lcsk_test_abc for calls"
	res := r.Redact(in, RedactionPointSnapshotText)
	if !res.HasMatch() {
		t.Fatal("expected api_key match")
	}
	if strings.Contains(res.Redacted, "sk_test_4eC39HqLyjWDarjtT1zdp7lcsk_test_abc") {
		t.Fatalf("api key not redacted: %q", res.Redacted)
	}
	if !contains(res.PatternsMatched, "api_key") {
		t.Fatalf("api_key not in matched: %v", res.PatternsMatched)
	}
}

func TestRedactBearerToken(t *testing.T) {
	r := mustRedactor(t, DefaultRedactionConfig())
	in := "Authorization: Bearer dGhpcyBpcyBhIHRva2Vu"
	res := r.Redact(in, RedactionPointSnapshotText)
	if !contains(res.PatternsMatched, "bearer_token") {
		t.Fatalf("bearer_token not matched: %v", res.PatternsMatched)
	}
	if strings.Contains(res.Redacted, "dGhpcyBpcyBhIHRva2Vu") {
		t.Fatalf("bearer token not redacted: %q", res.Redacted)
	}
}

func TestRedactPassword(t *testing.T) {
	r := mustRedactor(t, DefaultRedactionConfig())
	in := "password=hunter2secretvalue"
	res := r.Redact(in, RedactionPointSnapshotText)
	if !contains(res.PatternsMatched, "password") {
		t.Fatalf("password not matched: %v", res.PatternsMatched)
	}
	if strings.Contains(res.Redacted, "hunter2secretvalue") {
		t.Fatalf("password not redacted: %q", res.Redacted)
	}
}

func TestRedactSSN(t *testing.T) {
	r := mustRedactor(t, DefaultRedactionConfig())
	in := "SSN is 123-45-6789 on file"
	res := r.Redact(in, RedactionPointSnapshotText)
	if !contains(res.PatternsMatched, "ssn") {
		t.Fatalf("ssn not matched: %v", res.PatternsMatched)
	}
	if strings.Contains(res.Redacted, "123-45-6789") {
		t.Fatalf("ssn not redacted: %q", res.Redacted)
	}
}

func TestRedactCreditCard(t *testing.T) {
	r := mustRedactor(t, DefaultRedactionConfig())
	in := "card 4111 1111 1111 1111 expires 12/25"
	res := r.Redact(in, RedactionPointSnapshotText)
	if !contains(res.PatternsMatched, "credit_card") {
		t.Fatalf("credit_card not matched: %v", res.PatternsMatched)
	}
	if strings.Contains(res.Redacted, "4111 1111 1111 1111") {
		t.Fatalf("credit card not redacted: %q", res.Redacted)
	}
}

func TestRedactEmail(t *testing.T) {
	r := mustRedactor(t, DefaultRedactionConfig())
	in := "contact alice@example.com for help"
	res := r.Redact(in, RedactionPointSnapshotText)
	if !contains(res.PatternsMatched, "email") {
		t.Fatalf("email not matched: %v", res.PatternsMatched)
	}
	if strings.Contains(res.Redacted, "alice@example.com") {
		t.Fatalf("email not redacted: %q", res.Redacted)
	}
}

func TestRedactPhone(t *testing.T) {
	r := mustRedactor(t, DefaultRedactionConfig())
	in := "call +1 (555) 123-4567 today"
	res := r.Redact(in, RedactionPointSnapshotText)
	if !contains(res.PatternsMatched, "phone") {
		t.Fatalf("phone not matched: %v", res.PatternsMatched)
	}
	if strings.Contains(res.Redacted, "(555) 123-4567") {
		t.Fatalf("phone not redacted: %q", res.Redacted)
	}
}

func TestValidateURLPoint(t *testing.T) {
	r := mustRedactor(t, DefaultRedactionConfig())
	res := r.ValidateURL("https://api.example.com/data?api_key=sk_test_abcdefghijklmnopqrstuv")
	if res.Point != RedactionPointURLValidation {
		t.Fatalf("point = %q, want %q", res.Point, RedactionPointURLValidation)
	}
	if !res.Changed() {
		t.Fatal("url must be redacted")
	}
	if strings.Contains(res.Redacted, "sk_test_abcdefghijklmnopqrstuv") {
		t.Fatalf("url secret not redacted: %q", res.Redacted)
	}
}

func TestRedactSnapshotPoint(t *testing.T) {
	r := mustRedactor(t, DefaultRedactionConfig())
	res := r.RedactSnapshot("token: bearer abcdefghijklmnop123456")
	if res.Point != RedactionPointSnapshotText {
		t.Fatalf("point = %q, want %q", res.Point, RedactionPointSnapshotText)
	}
}

func TestRedactVisionOutputPoint(t *testing.T) {
	r := mustRedactor(t, DefaultRedactionConfig())
	res := r.RedactVisionOutput("the image shows email bob@corp.io")
	if res.Point != RedactionPointVisionOutput {
		t.Fatalf("point = %q, want %q", res.Point, RedactionPointVisionOutput)
	}
	if !res.Changed() {
		t.Fatal("vision output must be redacted")
	}
}

func TestRedactDisabledNoChange(t *testing.T) {
	cfg := DefaultRedactionConfig()
	cfg.Enabled = false
	r := mustRedactor(t, cfg)
	in := "api_key=sk_test_abcdefghijklmnopqrstuv1234"
	res := r.Redact(in, RedactionPointSnapshotText)
	if res.Changed() {
		t.Fatalf("disabled redactor must not change content: %q", res.Redacted)
	}
	if res.HasMatch() {
		t.Fatal("disabled redactor must report no matches")
	}
}

func TestRedactNoPatternsMatched(t *testing.T) {
	r := mustRedactor(t, DefaultRedactionConfig())
	in := "just ordinary prose with no secrets here"
	res := r.Redact(in, RedactionPointSnapshotText)
	if res.HasMatch() {
		t.Fatalf("expected no matches, got %v", res.PatternsMatched)
	}
	if res.Redacted != in {
		t.Fatal("content must be unchanged when no matches")
	}
}

func TestRedactEmptyPatternsConfig(t *testing.T) {
	r := mustRedactor(t, RedactionConfig{Enabled: true, Patterns: nil})
	res := r.Redact("api_key=sk_test_abcdefghijklmnopqrstuv1234", RedactionPointSnapshotText)
	if res.HasMatch() {
		t.Fatal("no patterns configured must not match")
	}
}

func TestRedactCustomReplacement(t *testing.T) {
	cfg := DefaultRedactionConfig()
	cfg.Replacement = "XXX"
	r := mustRedactor(t, cfg)
	res := r.Redact("email a@b.co", RedactionPointSnapshotText)
	if !strings.Contains(res.Redacted, "XXX") {
		t.Fatalf("custom replacement not applied: %q", res.Redacted)
	}
}

func TestRedactStats(t *testing.T) {
	r := mustRedactor(t, DefaultRedactionConfig())
	r.Redact("email a@b.co", RedactionPointSnapshotText)    // redacted
	r.Redact("nothing here", RedactionPointSnapshotText)    // not redacted
	r.Redact("ssn 111-22-3333", RedactionPointSnapshotText) // redacted
	s := r.Stats()
	if s.Total != 3 {
		t.Fatalf("Total = %d, want 3", s.Total)
	}
	if s.Redacted != 2 {
		t.Fatalf("Redacted = %d, want 2", s.Redacted)
	}
	if s.PatternsMatched < 2 {
		t.Fatalf("PatternsMatched = %d, want >= 2", s.PatternsMatched)
	}
}

func TestRedactCaseInsensitiveAPIKey(t *testing.T) {
	r := mustRedactor(t, DefaultRedactionConfig())
	res := r.Redact("API_KEY=sk_test_abcdefghijklmnopqrstuv1234", RedactionPointSnapshotText)
	if !contains(res.PatternsMatched, "api_key") {
		t.Fatalf("case-insensitive api_key not matched: %v", res.PatternsMatched)
	}
}

func TestRedactConcurrent(t *testing.T) {
	r := mustRedactor(t, DefaultRedactionConfig())
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				in := "email user" + string(rune('a'+seed)) + "@example.com api_key=sk_test_abcdefghijklmnopqrstuv1234"
				res := r.Redact(in, RedactionPointSnapshotText)
				if !res.Changed() {
					t.Errorf("goroutine %d: expected redaction", seed)
				}
				if strings.Contains(res.Redacted, "sk_test_abcdefghijklmnopqrstuv1234") {
					t.Errorf("goroutine %d: secret leaked", seed)
				}
			}
		}(g)
	}
	wg.Wait()
	s := r.Stats()
	if s.Total != 400 {
		t.Fatalf("Total = %d, want 400", s.Total)
	}
}

func TestRedactMultiplePatternsOneCall(t *testing.T) {
	r := mustRedactor(t, DefaultRedactionConfig())
	in := "email a@b.co and ssn 123-45-6789 and card 4111111111111111"
	res := r.Redact(in, RedactionPointSnapshotText)
	if len(res.PatternsMatched) < 3 {
		t.Fatalf("expected >=3 patterns matched, got %v", res.PatternsMatched)
	}
	if strings.Contains(res.Redacted, "a@b.co") || strings.Contains(res.Redacted, "123-45-6789") {
		t.Fatalf("not all secrets redacted: %q", res.Redacted)
	}
}

func TestRedactResultStringSafe(t *testing.T) {
	r := mustRedactor(t, DefaultRedactionConfig())
	res := r.Redact("api_key=sk_test_abcdefghijklmnopqrstuv1234", RedactionPointSnapshotText)
	s := res.String()
	if strings.Contains(s, "sk_test_abcdefghijklmnopqrstuv1234") {
		t.Fatalf("String() leaked secret: %q", s)
	}
}

func TestRedactDropsNilPatterns(t *testing.T) {
	cfg := RedactionConfig{
		Enabled: true,
		Patterns: []RedactionPattern{
			{Name: "good", Pattern: regexp.MustCompile(`secret`)},
			{Name: "bad", Pattern: nil},
		},
	}
	r := mustRedactor(t, cfg)
	res := r.Redact("a secret here", RedactionPointSnapshotText)
	if !contains(res.PatternsMatched, "good") {
		t.Fatalf("good pattern not matched: %v", res.PatternsMatched)
	}
	if contains(res.PatternsMatched, "bad") {
		t.Fatal("nil pattern must be dropped")
	}
}

func TestDefaultRedactionConfigEnabled(t *testing.T) {
	c := DefaultRedactionConfig()
	if !c.Enabled {
		t.Fatal("default config must be enabled")
	}
	if len(c.Patterns) < 7 {
		t.Fatalf("expected >=7 default patterns, got %d", len(c.Patterns))
	}
	if c.Replacement == "" {
		t.Fatal("default replacement must be set")
	}
}

func TestRedactConfigCopy(t *testing.T) {
	r := mustRedactor(t, DefaultRedactionConfig())
	c1 := r.Config()
	c1.Enabled = false
	c2 := r.Config()
	if !c2.Enabled {
		t.Fatal("Config() must return a copy, not a live reference")
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
