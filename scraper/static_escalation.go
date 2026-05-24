package scraper

// RenderMode selects static vs escalated browser rendering.
type RenderMode string

const (
	RenderModeStatic     RenderMode = "static"
	RenderModeEscalated  RenderMode = "escalated"
)

// StaticRenderlessEscalation decides when static fetch must escalate to full browser.
func StaticRenderlessEscalation(statusCode int, bodyLen int, infiniteScroll bool) RenderMode {
	if statusCode >= 400 || bodyLen < 64 {
		return RenderModeEscalated
	}
	if infiniteScroll {
		return RenderModeEscalated
	}
	return RenderModeStatic
}
