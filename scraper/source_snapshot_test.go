package scraper

import "testing"

func TestSourceSnapshotHasOutputContractFields(t *testing.T) {
	// Verify the spec L3990 output contract fields exist on SourceSnapshot.
	s := SourceSnapshot{
		URL:                  "https://example.com",
		HTML:                 "<html></html>",
		Mode:                 "renderless_js",
		RenderMode:           "renderless_js",
		FallbackReason:       "layout_query_reliance",
		UnsupportedFeatures:  []string{"getBoundingClientRect"},
		RenderlessWebAPIHits: []string{"fetch", "XMLHttpRequest"},
		RequestIntercepts:    []RequestIntercept{{URL: "https://api.example.com/data", Method: "GET", Action: "continue"}},
	}
	timeoutMs := 5000
	s.ScriptTimeout = &timeoutMs

	if s.RenderMode != "renderless_js" {
		t.Errorf("RenderMode = %q, want renderless_js", s.RenderMode)
	}
	if s.FallbackReason != "layout_query_reliance" {
		t.Errorf("FallbackReason = %q, want layout_query_reliance", s.FallbackReason)
	}
	if len(s.UnsupportedFeatures) != 1 {
		t.Errorf("UnsupportedFeatures len = %d, want 1", len(s.UnsupportedFeatures))
	}
	if len(s.RenderlessWebAPIHits) != 2 {
		t.Errorf("RenderlessWebAPIHits len = %d, want 2", len(s.RenderlessWebAPIHits))
	}
	if s.ScriptTimeout == nil || *s.ScriptTimeout != 5000 {
		t.Errorf("ScriptTimeout = %v, want 5000", s.ScriptTimeout)
	}
	if len(s.RequestIntercepts) != 1 {
		t.Errorf("RequestIntercepts len = %d, want 1", len(s.RequestIntercepts))
	}
	if s.RequestIntercepts[0].URL != "https://api.example.com/data" {
		t.Errorf("RequestIntercepts[0].URL = %q, want https://api.example.com/data", s.RequestIntercepts[0].URL)
	}
}

func TestSourceSnapshotZeroValueOmit(t *testing.T) {
	// Zero-value SourceSnapshot should have omitempty fields absent
	s := SourceSnapshot{
		URL:  "https://example.com",
		HTML: "<html></html>",
		Mode: "static_fetch",
	}
	if s.RenderMode != "" {
		t.Errorf("RenderMode = %q, want empty", s.RenderMode)
	}
	if s.FallbackReason != "" {
		t.Errorf("FallbackReason = %q, want empty", s.FallbackReason)
	}
	if s.UnsupportedFeatures != nil {
		t.Errorf("UnsupportedFeatures = %v, want nil", s.UnsupportedFeatures)
	}
	if s.RenderlessWebAPIHits != nil {
		t.Errorf("RenderlessWebAPIHits = %v, want nil", s.RenderlessWebAPIHits)
	}
	if s.ScriptTimeout != nil {
		t.Errorf("ScriptTimeout = %v, want nil", s.ScriptTimeout)
	}
	if s.RequestIntercepts != nil {
		t.Errorf("RequestIntercepts = %v, want nil", s.RequestIntercepts)
	}
}

func TestRequestIntercept(t *testing.T) {
	ri := RequestIntercept{
		URL:    "https://example.com/api",
		Method: "POST",
		Action: "fulfill",
		Status: 200,
	}
	if ri.URL != "https://example.com/api" {
		t.Errorf("URL = %q", ri.URL)
	}
	if ri.Method != "POST" {
		t.Errorf("Method = %q", ri.Method)
	}
	if ri.Action != "fulfill" {
		t.Errorf("Action = %q", ri.Action)
	}
	if ri.Status != 200 {
		t.Errorf("Status = %d", ri.Status)
	}
}
