package scraper

import "testing"

func TestStaticRenderlessEscalation(t *testing.T) {
	if got := StaticRenderlessEscalation(200, 1024, false); got != RenderModeStatic {
		t.Fatalf("got %s", got)
	}
	if got := StaticRenderlessEscalation(404, 1024, false); got != RenderModeEscalated {
		t.Fatalf("got %s", got)
	}
	if got := StaticRenderlessEscalation(200, 1024, true); got != RenderModeEscalated {
		t.Fatalf("got %s", got)
	}
}
