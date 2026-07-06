package security

import (
	"context"
	"testing"
)

func TestResolveConsentModeDefaultsManual(t *testing.T) {
	if m := ResolveConsentMode(""); m != ConsentModeManual {
		t.Fatalf("empty must default manual, got %s", m)
	}
	if m := ResolveConsentMode("garbage"); m != ConsentModeManual {
		t.Fatalf("unknown must default manual, got %s", m)
	}
	if m := ResolveConsentMode("auto_accept"); m != ConsentModeAutoAccept {
		t.Fatalf("auto_accept must parse, got %s", m)
	}
	if m := ResolveConsentMode("REJECT_NONESSENTIAL"); m != ConsentModeRejectNonEssent {
		t.Fatalf("reject_nonessential must parse case-insensitive, got %s", m)
	}
}

func TestAutoConfirmNoBannerReturnsDeny(t *testing.T) {
	page := ConsentPage{
		URL: "https://example.com/page",
		Elements: []ConsentElement{
			{Tag: "div", Text: "Hello world", Style: "position:static"},
		},
	}
	dec, err := AutoConfirm(context.Background(), page, ConsentProfile{Mode: ConsentModeAutoAccept, AllowedDomains: []string{"example.com"}, Purpose: "scrape"})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != ConsentDeny {
		t.Fatalf("expected deny, got %s", dec.Action)
	}
	if dec.BannerFound {
		t.Fatal("banner must not be found")
	}
	if dec.Reason != "no_cookie_banner" {
		t.Fatalf("expected no_cookie_banner, got %s", dec.Reason)
	}
}

func TestAutoConfirmBannerFixedPositionWithKeyword(t *testing.T) {
	page := ConsentPage{
		URL: "https://shop.example.com/",
		Elements: []ConsentElement{
			{
				Tag:   "div",
				Text:  "We use cookies",
				Style: "position:fixed;bottom:0",
				Children: []ConsentElement{
					{Tag: "button", Text: "Accept all"},
				},
			},
		},
	}
	dec, err := AutoConfirm(context.Background(), page, ConsentProfile{Mode: ConsentModeAutoAccept, AllowedDomains: []string{"example.com"}, Purpose: "scrape"})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.BannerFound {
		t.Fatal("banner must be found")
	}
	if dec.AcceptButtonIdx < 0 {
		t.Fatal("accept button must be found")
	}
	if dec.Action != ConsentAccept {
		t.Fatalf("expected accept, got %s (reason=%s)", dec.Action, dec.Reason)
	}
}

func TestAutoConfirmBannerZIndexAttr(t *testing.T) {
	page := ConsentPage{
		URL: "https://shop.example.com/",
		Elements: []ConsentElement{
			{
				Tag:   "div",
				Text:  "Datenschutz-Einstellungen",
				Attrs: map[string]string{"z-index": "9999"},
				Children: []ConsentElement{
					{Tag: "button", Text: "Akzeptieren"},
				},
			},
		},
	}
	dec, _ := AutoConfirm(context.Background(), page, ConsentProfile{Mode: ConsentModeAutoAccept, AllowedDomains: []string{"example.com"}, Purpose: "scrape"})
	if !dec.BannerFound {
		t.Fatal("banner must be found via z-index attr")
	}
	if dec.Action != ConsentAccept {
		t.Fatalf("expected accept, got %s (reason=%s)", dec.Action, dec.Reason)
	}
}

func TestAutoConfirmBannerZIndexInStyle(t *testing.T) {
	page := ConsentPage{
		URL: "https://shop.example.com/",
		Elements: []ConsentElement{
			{
				Tag:   "div",
				Text:  "Cookie consent",
				Style: "z-index: 1500",
				Children: []ConsentElement{
					{Tag: "button", Text: "Allow all"},
				},
			},
		},
	}
	dec, _ := AutoConfirm(context.Background(), page, ConsentProfile{Mode: ConsentModeAutoAccept, AllowedDomains: []string{"example.com"}, Purpose: "scrape"})
	if !dec.BannerFound {
		t.Fatal("banner must be found via z-index in style")
	}
}

func TestAutoConfirmBannerLowZIndexNotMatched(t *testing.T) {
	page := ConsentPage{
		URL: "https://shop.example.com/",
		Elements: []ConsentElement{
			{
				Tag:   "div",
				Text:  "Cookie banner",
				Style: "z-index: 100",
				Children: []ConsentElement{
					{Tag: "button", Text: "Accept"},
				},
			},
		},
	}
	dec, _ := AutoConfirm(context.Background(), page, ConsentProfile{Mode: ConsentModeAutoAccept, AllowedDomains: []string{"example.com"}, Purpose: "scrape"})
	if dec.BannerFound {
		t.Fatal("low z-index banner must NOT be matched")
	}
}

func TestAutoConfirmBannerKeywordNoPositionNotMatched(t *testing.T) {
	page := ConsentPage{
		URL: "https://shop.example.com/",
		Elements: []ConsentElement{
			{
				Tag:   "div",
				Text:  "We use cookies",
				Style: "position:absolute",
				Children: []ConsentElement{
					{Tag: "button", Text: "Accept"},
				},
			},
		},
	}
	dec, _ := AutoConfirm(context.Background(), page, ConsentProfile{Mode: ConsentModeAutoAccept, AllowedDomains: []string{"example.com"}, Purpose: "scrape"})
	if dec.BannerFound {
		t.Fatal("absolute-position banner must NOT be matched")
	}
}

func TestAutoConfirmBannerNoAcceptButton(t *testing.T) {
	page := ConsentPage{
		URL: "https://shop.example.com/",
		Elements: []ConsentElement{
			{
				Tag:   "div",
				Text:  "Cookie consent",
				Style: "position:fixed",
				Children: []ConsentElement{
					{Tag: "button", Text: "Settings"},
					{Tag: "button", Text: "Manage"},
				},
			},
		},
	}
	dec, _ := AutoConfirm(context.Background(), page, ConsentProfile{Mode: ConsentModeAutoAccept, AllowedDomains: []string{"example.com"}, Purpose: "scrape"})
	if !dec.BannerFound {
		t.Fatal("banner must be found")
	}
	if dec.Action != ConsentDeny {
		t.Fatalf("expected deny, got %s", dec.Action)
	}
	if dec.Reason != "no_accept_button" {
		t.Fatalf("expected no_accept_button, got %s", dec.Reason)
	}
}

func TestAutoConfirmModeManualDenies(t *testing.T) {
	page := ConsentPage{
		URL: "https://shop.example.com/",
		Elements: []ConsentElement{
			{
				Tag:   "div",
				Text:  "We use cookies",
				Style: "position:fixed",
				Children: []ConsentElement{
					{Tag: "button", Text: "Accept"},
				},
			},
		},
	}
	dec, _ := AutoConfirm(context.Background(), page, ConsentProfile{Mode: ConsentModeManual, AllowedDomains: []string{"example.com"}, Purpose: "scrape"})
	if dec.Action != ConsentDeny {
		t.Fatalf("manual mode must deny, got %s", dec.Action)
	}
	if dec.Reason != "consent_mode_not_auto_accept" {
		t.Fatalf("expected consent_mode_not_auto_accept, got %s", dec.Reason)
	}
}

func TestAutoConfirmDomainNotInAllowlist(t *testing.T) {
	page := ConsentPage{
		URL: "https://shop.example.com/",
		Elements: []ConsentElement{
			{
				Tag:   "div",
				Text:  "We use cookies",
				Style: "position:fixed",
				Children: []ConsentElement{
					{Tag: "button", Text: "Accept"},
				},
			},
		},
	}
	dec, _ := AutoConfirm(context.Background(), page, ConsentProfile{Mode: ConsentModeAutoAccept, AllowedDomains: []string{"other.com"}, Purpose: "scrape"})
	if dec.Action != ConsentDeny {
		t.Fatalf("non-allowlisted domain must deny, got %s", dec.Action)
	}
	if dec.Reason != "domain_not_in_allowlist" {
		t.Fatalf("expected domain_not_in_allowlist, got %s", dec.Reason)
	}
}

func TestAutoConfirmSubdomainInAllowlist(t *testing.T) {
	page := ConsentPage{
		URL: "https://shop.example.com/path",
		Elements: []ConsentElement{
			{
				Tag:   "div",
				Text:  "We use cookies",
				Style: "position:fixed",
				Children: []ConsentElement{
					{Tag: "button", Text: "Accept"},
				},
			},
		},
	}
	dec, _ := AutoConfirm(context.Background(), page, ConsentProfile{Mode: ConsentModeAutoAccept, AllowedDomains: []string{"example.com"}, Purpose: "scrape"})
	if dec.Action != ConsentAccept {
		t.Fatalf("subdomain of allowlisted root must accept, got %s (reason=%s)", dec.Action, dec.Reason)
	}
}

func TestAutoConfirmHighRiskDomainSkipped(t *testing.T) {
	page := ConsentPage{
		URL: "https://portal.bank.example.com/",
		Elements: []ConsentElement{
			{
				Tag:   "div",
				Text:  "We use cookies",
				Style: "position:fixed",
				Children: []ConsentElement{
					{Tag: "button", Text: "Accept"},
				},
			},
		},
	}
	dec, _ := AutoConfirm(context.Background(), page, ConsentProfile{Mode: ConsentModeAutoAccept, AllowedDomains: []string{"example.com"}, Purpose: "scrape"})
	if dec.Action != ConsentDeny {
		t.Fatalf("high-risk domain must deny, got %s", dec.Action)
	}
	if dec.Reason != "high_risk_domain_skip" {
		t.Fatalf("expected high_risk_domain_skip, got %s", dec.Reason)
	}
}

func TestAutoConfirmMissingPurposeDenies(t *testing.T) {
	page := ConsentPage{
		URL: "https://shop.example.com/",
		Elements: []ConsentElement{
			{
				Tag:   "div",
				Text:  "We use cookies",
				Style: "position:fixed",
				Children: []ConsentElement{
					{Tag: "button", Text: "Accept"},
				},
			},
		},
	}
	dec, _ := AutoConfirm(context.Background(), page, ConsentProfile{Mode: ConsentModeAutoAccept, AllowedDomains: []string{"example.com"}, Purpose: ""})
	if dec.Action != ConsentDeny {
		t.Fatalf("missing purpose must deny, got %s", dec.Action)
	}
	if dec.Reason != "missing_processing_purpose" {
		t.Fatalf("expected missing_processing_purpose, got %s", dec.Reason)
	}
}

func TestAutoConfirmContextCancelledDenies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	page := ConsentPage{
		URL: "https://shop.example.com/",
		Elements: []ConsentElement{
			{
				Tag:   "div",
				Text:  "We use cookies",
				Style: "position:fixed",
				Children: []ConsentElement{
					{Tag: "button", Text: "Accept"},
				},
			},
		},
	}
	dec, _ := AutoConfirm(ctx, page, ConsentProfile{Mode: ConsentModeAutoAccept, AllowedDomains: []string{"example.com"}, Purpose: "scrape"})
	if dec.Action != ConsentDeny {
		t.Fatalf("cancelled ctx must deny, got %s", dec.Action)
	}
	if dec.Reason != "context_cancelled" {
		t.Fatalf("expected context_cancelled, got %s", dec.Reason)
	}
}

func TestAutoConfirmNilElementsErrors(t *testing.T) {
	_, err := AutoConfirm(context.Background(), ConsentPage{URL: "https://x"}, ConsentProfile{})
	if err == nil {
		t.Fatal("nil elements must error")
	}
}

func TestAutoConfirmLargestAcceptButtonChosen(t *testing.T) {
	// Two accept buttons; the LARGEST by text length must win.
	page := ConsentPage{
		URL: "https://shop.example.com/",
		Elements: []ConsentElement{
			{
				Tag:   "div",
				Text:  "We use cookies",
				Style: "position:fixed",
				Children: []ConsentElement{
					{Tag: "button", Text: "OK"},
					{Tag: "button", Text: "Accept all cookies"},
				},
			},
		},
	}
	dec, _ := AutoConfirm(context.Background(), page, ConsentProfile{Mode: ConsentModeAutoAccept, AllowedDomains: []string{"example.com"}, Purpose: "scrape"})
	if dec.Action != ConsentAccept {
		t.Fatalf("expected accept, got %s (reason=%s)", dec.Action, dec.Reason)
	}
}

func TestAutoConfirmGermanBannerAndButton(t *testing.T) {
	page := ConsentPage{
		URL: "https://shop.example.com/",
		Elements: []ConsentElement{
			{
				Tag:   "div",
				Text:  "Datenschutz-Einstellungen",
				Style: "position:sticky;top:0",
				Children: []ConsentElement{
					{Tag: "button", Text: "Alle akzeptieren"},
				},
			},
		},
	}
	dec, _ := AutoConfirm(context.Background(), page, ConsentProfile{Mode: ConsentModeAutoAccept, AllowedDomains: []string{"example.com"}, Purpose: "scrape"})
	if dec.Action != ConsentAccept {
		t.Fatalf("expected accept for German banner, got %s (reason=%s)", dec.Action, dec.Reason)
	}
}
