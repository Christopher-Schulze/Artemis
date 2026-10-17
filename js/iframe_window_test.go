package js

import (
	"context"
	"strings"
	"testing"
)

func TestIFrameFailuresCannotAcknowledgePartialSuccess(t *testing.T) {
	for _, test := range []struct {
		name, source, expression, message string
	}{
		{"inline", `<script>throw new Error('frame-script-failed')</script><script>console.log('must-not-run')</script>`, `try { document.querySelector('iframe').contentDocument } catch (_) {}`, "frame-script-failed"},
		{"message", `<script>addEventListener('message', () => { document.title = 'changed'; throw new Error('frame-listener-failed') })</script>`, `try { document.querySelector('iframe').contentWindow.postMessage('run') } catch (_) {}`, "frame-listener-failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			console := &CollectConsole{}
			c := newCtxFromHTMLOpts(t, `<iframe src="/child"></iframe>`, ContextOpts{
				Console: console,
				LoadIFrame: func(ctx context.Context, target string) (*IFrameResponse, error) {
					return &IFrameResponse{URL: target, Body: []byte("<title>before</title>" + test.source)}, nil
				},
			})
			if test.name == "message" {
				value, err := c.Eval(t.Context(), `document.querySelector('iframe').contentDocument`)
				if err != nil {
					t.Fatal(err)
				}
				value.Raw().Release()
			}
			if value, err := c.Eval(t.Context(), test.expression); value != nil || err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("frame failure became partial success: value=%v err=%v", value, err)
			}
			if len(console.Snapshot()) != 0 {
				t.Fatal("frame execution continued after its script failed")
			}
			if test.name == "message" {
				c.iframes.mu.Lock()
				for _, doc := range c.iframes.docs {
					if doc.Title() != "changed" {
						t.Error("actual child listener did not execute before reporting failure")
					}
				}
				c.iframes.mu.Unlock()
			}
			assertExecutionRecovery(t, c)
		})
	}
}

func TestWindowParentTopSelf(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(window.parent === window) + ':' +
		(window.top === window) + ':' +
		(window.self === window) + ':' +
		(window.opener === null)
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "true:true:true:true" {
		t.Errorf("got %q", v.String())
	}
}

func TestWindowFramesLive(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><iframe></iframe><iframe></iframe></body></html>`, nil)
	v, err := c.Eval(context.Background(), `window.frames.length`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.Int64() != 2 {
		t.Errorf("got %d, want 2", v.Int64())
	}
}
