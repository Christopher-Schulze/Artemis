package actions

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
)

func TestScreenshotElementRefCapturesFreshRealPixels(t *testing.T) {
	f := newActionFixture(t)
	var event bridge.ImageMetricEvent
	f.runtime.imageMetrics = func(observation bridge.ImageMetricEvent) { event = observation }
	requireAction(t, f.runtime.Execute(t.Context(), Request{Kind: KindEvaluate, Expression: `document.body.style.background='rgb(10,190,30)';const b=document.querySelector('[aria-label="Counter"]');b.textContent='';b.style.cssText='position:absolute;left:40px;top:30px;width:120px;height:90px;background:rgb(220,30,40);border:0;padding:0';document.querySelector('iframe').style.cssText='position:absolute;left:250px;top:300px;width:300px;height:150px;border:5px solid';const c=document.querySelector('iframe').contentDocument.querySelector('button');c.textContent='';c.style.cssText='width:100px;height:60px;background:rgb(220,30,40);border:0;padding:0';const s=document.querySelector('#host').shadowRoot.querySelector('button');s.textContent='';s.style.cssText='position:absolute;left:40px;top:500px;width:80px;height:50px;background:rgb(220,30,40);border:0;padding:0'`}))
	snapshot, err := f.observer.Capture(t.Context(), bridgeobserve.ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	refs := make(map[string]string)
	for _, node := range snapshot.Nodes {
		if node.Ref != "" {
			refs[node.Name] = node.Ref
		}
	}
	for _, test := range []struct {
		name          string
		width, height int
		format        string
	}{
		{"Counter", 120, 90, "png"},
		{"Frame action", 100, 60, "jpeg"},
		{"Shadow click", 80, 50, "png"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if refs[test.name] == "" {
				t.Fatal("actual fixture element reference missing")
			}
			out := f.runtime.Execute(t.Context(), Request{Kind: KindScreenshot, Ref: refs[test.name], Format: test.format})
			requireAction(t, out)
			pixels, format, err := image.Decode(bytes.NewReader(out.Bytes))
			if err != nil || format != test.format || out.Width != test.width || out.Height != test.height || pixels.Bounds().Dx() != test.width || pixels.Bounds().Dy() != test.height || out.Evidence.BackendDOMID <= 0 || out.Evidence.FrameID == "" || event.CaptureAttempts != 1 {
				t.Fatalf("element crop replaced by wrong dimensions/source: %dx%d %s %v %+v", out.Width, out.Height, format, err, out.Evidence)
			}
			red, green, blue, _ := pixels.At(10, 10).RGBA()
			if red < 200*257 || green > 60*257 || blue > 70*257 {
				t.Fatal("capture included neighboring green page instead of the red element")
			}
		})
	}
	requireAction(t, f.runtime.Execute(t.Context(), Request{Kind: KindEvaluate, Expression: `(()=>{const b=document.querySelector('[aria-label="Counter"]');b.style.top='1400px';b.style.width='140px';b.style.height='70px';scrollTo(0,1000)})()`}))
	moved := f.runtime.Execute(t.Context(), Request{Kind: KindScreenshot, Ref: refs["Counter"], Format: "png"})
	requireAction(t, moved)
	pixels, _, err := image.Decode(bytes.NewReader(moved.Bytes))
	if err != nil || moved.Width != 140 || moved.Height != 70 {
		t.Fatalf("screenshot reused stale layout after movement/scroll: %dx%d %v", moved.Width, moved.Height, err)
	}
	red, green, _, _ := pixels.At(10, 10).RGBA()
	if red < 200*257 || green > 60*257 {
		t.Fatal("scrolled element crop used viewport coordinates as document coordinates")
	}
	requireAction(t, f.runtime.Execute(t.Context(), Request{Kind: KindEvaluate, Expression: `document.querySelector('[aria-label="Counter"]').style.display='none'`}))
	for _, ref := range []string{refs["Counter"], "e999999"} {
		out := f.runtime.Execute(t.Context(), Request{Kind: KindScreenshot, Ref: ref})
		if out.Success || len(out.Bytes) != 0 || event.CaptureAttempts != 0 || event.DecodeCounts != [3]uint32{} {
			t.Fatalf("hidden or unknown reference captured a whole page: %+v %+v", out, event)
		}
	}
}

func TestScreenshotElementRefCapturesOwnedOOPIFPixels(t *testing.T) {
	f := newActionFixture(t, "--site-per-process")
	var event bridge.ImageMetricEvent
	f.runtime.imageMetrics = func(observation bridge.ImageMetricEvent) { event = observation }
	childListener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp6", fmt.Sprintf("[::1]:%d", actionTestURLPort(t, f.server.URL)))
	if err != nil {
		t.Fatalf("IPv6 loopback OOPIF fixture: %v", err)
	}
	child := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := fmt.Fprint(w, `<!doctype html><style>body{height:1000px}</style><button aria-label="Counter">Child</button>`); err != nil {
			t.Error(err)
		}
	}))
	child.Listener = childListener
	child.Start()
	defer child.Close()
	childURL := child.URL + "/"
	requireAction(t, f.runtime.Execute(t.Context(), Request{Kind: KindEvaluate, Expression: fmt.Sprintf(`(()=>{const f=document.querySelector('iframe');f.style.cssText='position:absolute;left:60px;top:80px;width:300px;height:150px;margin:0;border:5px solid';f.removeAttribute('srcdoc');f.src=%q})()`, childURL)}))
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	frameID := ""
	for frameID == "" {
		for id := range f.page.FrameSessions() {
			var result runtimeCallResult
			if err := f.page.CallFrame(ctx, id, "Runtime.evaluate", runtimeEvaluateParams{Expression: `document.readyState==='complete' && !!document.querySelector('[aria-label="Counter"]')`, ReturnByValue: true}, &result); err == nil {
				var ready bool
				if result.valueInto(&ready) == nil && ready {
					frameID = id
					break
				}
			}
		}
		if frameID == "" {
			select {
			case <-ctx.Done():
				t.Fatal("actual cross-site OOPIF was not attached and ready")
			case <-time.After(25 * time.Millisecond):
			}
		}
	}
	requireAction(t, f.runtime.Execute(t.Context(), Request{Kind: KindFrameEvaluate, FrameID: frameID, Expression: `(()=>{const b=document.querySelector('[aria-label="Counter"]');document.body.style.background='rgb(10,190,30)';b.setAttribute('aria-label','OOPIF crop');b.textContent='';b.style.cssText='position:absolute;left:10px;top:10px;width:120px;height:90px;background:rgb(220,30,40);border:0;padding:0'})()`}))
	snapshot, err := f.observer.Capture(t.Context(), bridgeobserve.ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	ref := ""
	for _, node := range snapshot.Nodes {
		if node.FrameID == frameID && node.Name == "OOPIF crop" && node.Ref != "" {
			ref = node.Ref
		}
	}
	if ref == "" {
		t.Fatal("owned cross-renderer target reference missing")
	}
	assertOOPIFCrop := func(width, height int) {
		t.Helper()
		out := f.runtime.Execute(t.Context(), Request{Kind: KindScreenshot, Ref: ref})
		requireAction(t, out)
		pixels, _, err := image.Decode(bytes.NewReader(out.Bytes))
		if err != nil || out.Width != width || out.Height != height || out.Evidence.FrameID != frameID {
			t.Fatalf("OOPIF crop lost frame origin or viewport clipping: %dx%d %v", out.Width, out.Height, err)
		}
		for _, point := range []image.Point{{1, 1}, {width - 2, height - 2}} {
			red, green, blue, _ := pixels.At(point.X, point.Y).RGBA()
			if red < 200*257 || green > 60*257 || blue > 70*257 {
				t.Fatal("OOPIF capture included the parent or neighboring green pixels")
			}
		}
	}
	assertOOPIFCrop(120, 90)
	requireAction(t, f.runtime.Execute(t.Context(), Request{Kind: KindFrameEvaluate, FrameID: frameID, Expression: `(()=>{const b=document.querySelector('[aria-label="OOPIF crop"]');b.style.left='250px';b.style.top='100px'})()`}))
	var metrics struct {
		Viewport struct {
			Width  int `json:"clientWidth"`
			Height int `json:"clientHeight"`
		} `json:"cssLayoutViewport"`
	}
	if err := f.page.CallFrame(t.Context(), frameID, "Page.getLayoutMetrics", emptyParams{}, &metrics); err != nil {
		t.Fatal(err)
	}
	if metrics.Viewport.Width <= 250 || metrics.Viewport.Height <= 100 {
		t.Fatal("fixture child viewport unexpectedly smaller than the partial target")
	}
	assertOOPIFCrop(metrics.Viewport.Width-250, metrics.Viewport.Height-100)
	requireAction(t, f.runtime.Execute(t.Context(), Request{Kind: KindEvaluate, Expression: `document.querySelector('iframe').style.transform='rotate(10deg)'`}))
	out := f.runtime.Execute(t.Context(), Request{Kind: KindScreenshot, Ref: ref})
	if out.Success || out.Failure != FailureActionability || len(out.Bytes) != 0 || event.CaptureAttempts != 0 {
		t.Fatalf("unverified transformed-frame geometry captured unrelated pixels: %+v %+v", out, event)
	}
}
