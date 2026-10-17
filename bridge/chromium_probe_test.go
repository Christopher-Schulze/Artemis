package bridge

import (
	"context"
	"testing"
)

func TestChromiumProbeRequiresInitializedTransport(t *testing.T) {
	tests := []struct {
		name    string
		browser *ChromiumBrowser
		ctx     context.Context
	}{
		{"nil browser", nil, context.Background()},
		{"nil context", &ChromiumBrowser{}, nil},
		{"uninitialized", &ChromiumBrowser{}, context.Background()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.browser.Probe(test.ctx); err == nil {
				t.Fatal("missing transport or context was reported healthy")
			}
		})
	}
}
