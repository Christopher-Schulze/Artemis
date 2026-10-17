package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/bridge"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
)

// Inject only a protocol failure; all successful calls use the live renderer.
type failingOOPIFCaller struct {
	*bridge.Page
	frame, method string
}

func (c *failingOOPIFCaller) Call(ctx context.Context, method string, params, result any) error {
	if method == c.method && (method == "DOM.getFrameOwner" || method == "DOM.getBoxModel") {
		return fmt.Errorf("injected OOPIF protocol denial")
	}
	return c.Page.Call(ctx, method, params, result)
}

func (c *failingOOPIFCaller) CallFrame(ctx context.Context, frameID, method string, params, result any) error {
	if frameID == c.frame && method == c.method {
		return fmt.Errorf("injected OOPIF protocol denial")
	}
	return c.Page.CallFrame(ctx, frameID, method, params, result)
}

func oopifEvaluate(t *testing.T, ctx context.Context, page *bridge.Page, frameID, expression string) any {
	t.Helper()
	var result runtimeCallResult
	if err := page.CallFrame(ctx, frameID, "Runtime.evaluate", runtimeEvaluateParams{Expression: expression, ReturnByValue: true, AwaitPromise: true}, &result); err != nil {
		t.Fatal(err)
	}
	value, err := result.value()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func oopifButton(t *testing.T, snapshot bridgeobserve.Snapshot, name string) bridgeobserve.Node {
	t.Helper()
	for _, node := range snapshot.Nodes {
		if node.Role == "button" && node.Name == name {
			return node
		}
	}
	t.Fatalf("button %q absent: warnings=%v", name, snapshot.Warnings)
	return bridgeobserve.Node{}
}

func verifyOOPIFObservationFailures(t *testing.T, ctx context.Context, page *bridge.Page, frameID string) {
	t.Helper()
	for _, method := range []string{"Accessibility.enable", "DOM.enable", "DOMSnapshot.captureSnapshot", "Accessibility.getFullAXTree", "DOM.getDocument", "Page.getFrameTree", "DOM.getContentQuads", "DOM.getNodeForLocation", "Page.getLayoutMetrics", "DOM.getFrameOwner", "DOM.getBoxModel"} {
		t.Run(method, func(t *testing.T) {
			caller := &failingOOPIFCaller{Page: page, frame: frameID}
			collector, err := bridgeobserve.NewCollector(caller, bridgeobserve.DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			before, err := collector.Capture(ctx, bridgeobserve.ModeFull, "")
			if err != nil {
				t.Fatal(err)
			}
			ref := oopifButton(t, before, "OOPIF action").Ref
			caller.method = method
			after, err := collector.Capture(ctx, bridgeobserve.ModeFull, "")
			if err != nil {
				t.Fatal(err)
			}
			warnings := strings.Join(after.Warnings, " ")
			if !strings.Contains(warnings, "frame "+frameID+" "+method) || !strings.Contains(warnings, "injected OOPIF protocol denial") {
				t.Fatalf("frame/method failure hidden: %v", after.Warnings)
			}
			for _, node := range after.Nodes {
				if node.FrameID == frameID && node.Interactable {
					t.Fatalf("failed frame admitted an actionable node: %+v", node)
				}
			}
			runtime, err := NewRuntime(page, collector, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer closeActionTestResource(t, "failure runtime", runtime.Close)
			out := runtime.Execute(ctx, Request{Kind: KindClick, Ref: ref, Idempotent: true, RetryMax: 3})
			if out.Success || out.Evidence.Attempts != 1 || out.Evidence.ReplayAdmitted {
				t.Fatalf("unverified action admitted/replayed: %+v", out)
			}
			if count := oopifEvaluate(t, ctx, page, frameID, "document.querySelector('button').dataset.count"); count != "1" {
				t.Fatalf("denied action changed the real counter: %v", count)
			}
		})
	}
}

func verifyOOPIFPointerBoundaries(t *testing.T, ctx context.Context, page *bridge.Page, collector *bridgeobserve.Collector, runtime *Runtime, frameID string) {
	t.Helper()
	t.Run("native target subtree denial", func(t *testing.T) {
		caller := &failingOOPIFCaller{Page: page, frame: frameID}
		owned, err := bridgeobserve.NewCollector(caller, bridgeobserve.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		before, err := owned.Capture(ctx, bridgeobserve.ModeFull, "")
		if err != nil {
			t.Fatal(err)
		}
		ref := oopifButton(t, before, "OOPIF action").Ref
		caller.method = "DOM.describeNode"
		denied, err := NewRuntime(page, owned, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer closeActionTestResource(t, "subtree denial runtime", denied.Close)
		out := denied.Execute(ctx, Request{Kind: KindClick, Ref: ref})
		if out.Success || out.Evidence.Attempts != 1 || !strings.Contains(out.Error, "DOM.describeNode") {
			t.Fatalf("fresh native ancestry failure admitted input or lost cause: %+v", out)
		}
		if count := oopifEvaluate(t, ctx, page, frameID, "document.querySelector('button').dataset.count"); count != "1" {
			t.Fatalf("failed ancestry proof changed actual counter: %v", count)
		}
	})
	t.Run("frame query", func(t *testing.T) {
		request := bridgeobserve.QueryRequest{Options: bridgeobserve.QueryOptions{FrameID: frameID, Text: "OOPIF action", MaxNodes: 1, MaxBytes: 4096}}
		first, err := collector.Query(ctx, request)
		if err != nil || first.Kind != bridgeobserve.QueryFull || first.Lost || first.Snapshot == nil || len(first.Snapshot.Nodes) != 1 || first.Snapshot.Nodes[0].FrameID != frameID || !first.Snapshot.Nodes[0].Interactable {
			t.Fatalf("frame-scoped query invalid: %+v err=%v", first, err)
		}
		request.BaseRevision = first.Revision
		second, err := collector.Query(ctx, request)
		if err != nil || second.Kind != bridgeobserve.QueryUnchanged || second.Revision != first.Revision {
			t.Fatalf("unchanged OOPIF query invalid: %+v err=%v", second, err)
		}
	})
	for _, test := range []struct{ name, expression string }{
		{"OOPIF shadow", "document.querySelector('#host').shadowRoot.querySelector('button').dataset.count"},
		{"Nested action", "document.querySelector('iframe').contentDocument.querySelector('button').dataset.count"},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot, err := collector.Capture(ctx, bridgeobserve.ModeFull, "")
			if err != nil {
				t.Fatal(err)
			}
			node := oopifButton(t, snapshot, test.name)
			if !node.Interactable || node.Hit != bridgeobserve.HitClear {
				t.Fatalf("owned nested/shadow target unverified: %+v warnings=%v", node, snapshot.Warnings)
			}
			requireAction(t, runtime.Execute(ctx, Request{Kind: KindClick, Ref: node.Ref}))
			if count := oopifEvaluate(t, ctx, page, frameID, test.expression); count != "1" {
				t.Fatalf("real frame-routed effect missing: %v", count)
			}
		})
	}
	t.Run("scrolled viewports", func(t *testing.T) {
		oopifEvaluate(t, ctx, page, frameID, "document.querySelector('button').style.marginTop='300px';window.scrollTo(0,250)")
		oopifEvaluate(t, ctx, page, "", "window.scrollTo(0,70)")
		snapshot, err := collector.Capture(ctx, bridgeobserve.ModeFull, "")
		if err != nil {
			t.Fatal(err)
		}
		node := oopifButton(t, snapshot, "OOPIF action")
		if !node.Interactable || node.Hit != bridgeobserve.HitClear {
			var quads json.RawMessage
			quadErr := page.CallFrame(ctx, node.FrameID, "DOM.getContentQuads", resolveNodeParams{BackendNodeID: node.BackendNodeID}, &quads)
			childGeometry := oopifEvaluate(t, ctx, page, frameID, "JSON.stringify({rect:document.querySelector('button').getBoundingClientRect().toJSON(),scrollX,scrollY})")
			mainGeometry := oopifEvaluate(t, ctx, page, "", "JSON.stringify({rect:document.querySelector('iframe').getBoundingClientRect().toJSON(),scrollX,scrollY})")
			t.Fatalf("scrolled target unverified: %+v warnings=%v quads=%s quadErr=%v child=%v main=%v", node, snapshot.Warnings, quads, quadErr, childGeometry, mainGeometry)
		}
		requireAction(t, runtime.Execute(ctx, Request{Kind: KindClick, Ref: node.Ref}))
		if count := oopifEvaluate(t, ctx, page, frameID, "document.querySelector('button').dataset.count"); count != "2" {
			t.Fatalf("scrolled click effect=%v", count)
		}
		oopifEvaluate(t, ctx, page, frameID, "document.querySelector('button').style.marginTop='';window.scrollTo(0,0)")
		oopifEvaluate(t, ctx, page, "", "window.scrollTo(0,0)")
	})
	for _, coveringFrame := range []string{frameID, ""} {
		t.Run("covered/"+coveringFrame, func(t *testing.T) {
			oopifEvaluate(t, ctx, page, coveringFrame, "const overlay=document.createElement('div');overlay.id='cover';overlay.style='position:fixed;inset:0;z-index:99999;background:black';document.body.appendChild(overlay)")
			snapshot, err := collector.Capture(ctx, bridgeobserve.ModeFull, "")
			if err != nil {
				t.Fatal(err)
			}
			node := oopifButton(t, snapshot, "OOPIF action")
			if node.Interactable || node.Hit != bridgeobserve.HitCovered {
				t.Fatalf("overlay admitted a target: %+v warnings=%v", node, snapshot.Warnings)
			}
			out := runtime.Execute(ctx, Request{Kind: KindClick, Ref: node.Ref})
			if out.Success || out.Failure != FailureActionability {
				t.Fatalf("covered click admitted: %+v", out)
			}
			if count := oopifEvaluate(t, ctx, page, frameID, "document.querySelector('button').dataset.count"); count != "2" {
				t.Fatalf("covered click changed counter: %v", count)
			}
			oopifEvaluate(t, ctx, page, coveringFrame, "document.getElementById('cover').remove()")
		})
	}
	snapshot, err := collector.Capture(ctx, bridgeobserve.ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	node := oopifButton(t, snapshot, "OOPIF action")
	oopifEvaluate(t, ctx, page, "", "document.querySelector('iframe').remove()")
	var result runtimeCallResult
	if err := page.CallFrame(ctx, frameID, "Runtime.evaluate", runtimeEvaluateParams{Expression: "document.body.dataset.foreign='yes'", ReturnByValue: true}, &result); err == nil {
		t.Fatal("detached frame silently fell back to the main renderer")
	}
	if value := oopifEvaluate(t, ctx, page, "", "document.body.dataset.foreign"); value != nil {
		t.Fatalf("detached frame mutated main renderer: %v", value)
	}
	if out := runtime.Execute(ctx, Request{Kind: KindClick, Ref: node.Ref}); out.Success {
		t.Fatalf("detached target clicked: %+v", out)
	}
}
