package artemis

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

const visualPage = `<!doctype html><html><head><style>
body{margin:0;font:16px sans-serif;background:#fff}
button{position:absolute;width:120px;height:40px;border:0}
#alpha{left:20px;top:20px;background:#36c;color:#fff}
#beta{left:200px;top:20px;background:#3a3;color:#fff}
#paint{position:absolute;left:20px;top:120px}
#log{position:absolute;left:20px;top:240px;margin:0}
iframe{position:absolute;left:300px;top:120px;width:160px;height:100px;border:0}
#tall{height:3000px}
</style></head><body>
<button id="alpha" onclick="document.getElementById('log').textContent='clicked alpha'">Alpha</button>
<button id="beta" onclick="document.getElementById('log').textContent='clicked beta'">Beta</button>
<canvas id="paint" width="200" height="100"></canvas>
<iframe srcdoc="<body style='margin:0'><div style='height:60px;background:#c33'></div><div style='height:400px;background:#33c'></div></body>"></iframe>
<p id="log">idle</p>
<div id="fake" style="position:absolute;left:150px;top:300px;width:100px;height:30px;background:#999" onclick="document.getElementById('log').textContent='fake control'">Fake</div>
<div id="tall"></div>
<script>
const paint=document.getElementById('paint'),ctx=paint.getContext('2d');
ctx.fillStyle='#f80';ctx.fillRect(0,0,200,100);
paint.addEventListener('click',e=>{ctx.fillStyle='#000';ctx.fillRect(e.offsetX-10,e.offsetY-10,20,20);document.getElementById('log').textContent='canvas '+e.offsetX+','+e.offsetY});
</script></body></html>`

func startVisualSession(t *testing.T, grounder VisualGrounder) (*Session, string, context.Context) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, visualPage)
	}))
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	agent, err := NewAgent(AgentConfig{PolicyEngine: fixtureActionPolicy(t), Chromium: &browserprocess.LaunchConfig{Headless: true}, PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}}, VisualGrounder: grounder})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := agent.Stop(); err != nil {
			t.Error(err)
		}
	})
	session, err := agent.CreateSession("visual")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	t.Cleanup(cancel)
	pageID, _, taskErr := session.OpenChromiumPage(ctx, server.URL)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	return session, pageID, ctx
}

func visualAnnotation(t *testing.T, observation VisualObservation, name string) VisualAnnotation {
	t.Helper()
	for _, annotation := range observation.Annotations {
		if annotation.Name == name {
			return annotation
		}
	}
	t.Fatalf("no annotation %q in %+v", name, observation.Annotations)
	return VisualAnnotation{}
}

func center(annotation VisualAnnotation) (float64, float64) {
	return annotation.Box.X + annotation.Box.Width/2, annotation.Box.Y + annotation.Box.Height/2
}

func evaluate(t *testing.T, ctx context.Context, session *Session, pageID, expression string) {
	t.Helper()
	if outcome, taskErr := session.ActChromiumPage(ctx, pageID, 0, actions.Request{Kind: actions.KindEvaluate, Expression: expression}); taskErr != nil || !outcome.Success {
		t.Fatalf("evaluate %s: %+v %v", expression, outcome, taskErr)
	}
}

func observeVisual(t *testing.T, ctx context.Context, session *Session, pageID string, options VisualCaptureOptions) VisualObservation {
	t.Helper()
	observation, taskErr := session.CaptureVisual(ctx, pageID, options)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	return observation
}

func near(got, want float64) bool {
	return math.Abs(got-want) <= 1.5
}

func TestVisualObservationBindsPixelsReferencesAndCrops(t *testing.T) {
	session, pageID, ctx := startVisualSession(t, nil)
	plain := observeVisual(t, ctx, session, pageID, VisualCaptureOptions{})
	alpha := visualAnnotation(t, plain, "Alpha")
	if !near(alpha.Box.X, 20) || !near(alpha.Box.Y, 20) || !near(alpha.Box.Width, 120) || !near(alpha.Box.Height, 40) || !strings.HasPrefix(alpha.Ref, pageID+":") || alpha.Label != strings.TrimPrefix(alpha.Ref, pageID+":") {
		t.Fatalf("annotation not bound to the reference and viewport geometry: %+v", alpha)
	}
	if plain.Epoch == 0 || plain.Viewport.Width <= 0 || plain.Crop != nil || plain.MIME != "image/png" || plain.PixelRatio <= 0 {
		t.Fatalf("observation identity incomplete: %+v", plain)
	}
	pixels, err := png.Decode(bytes.NewReader(plain.Image))
	if err != nil {
		t.Fatal(err)
	}
	if pixels.Bounds().Dx() != plain.PixelWidth || !near(float64(plain.PixelWidth)/plain.PixelRatio, plain.Viewport.Width) {
		t.Fatalf("pixels do not cover the observed viewport: %v %+v", pixels.Bounds(), plain.Viewport)
	}
	inside := int((alpha.Box.X + 4) * plain.PixelRatio)
	if r, g, b, _ := pixels.At(inside, int((alpha.Box.Y+alpha.Box.Height/2)*plain.PixelRatio)).RGBA(); r>>8 > 80 || b>>8 < 150 || g>>8 > 140 {
		t.Fatalf("raw pixels at the alpha box are not the button: %d %d %d", r>>8, g>>8, b>>8)
	}

	annotated := observeVisual(t, ctx, session, pageID, VisualCaptureOptions{Annotate: true})
	drawn, err := png.Decode(bytes.NewReader(annotated.Image))
	if err != nil {
		t.Fatal(err)
	}
	box := visualAnnotation(t, annotated, "Alpha").Box
	if r, g, b, _ := drawn.At(int((box.X+box.Width/2)*annotated.PixelRatio), int(box.Y*annotated.PixelRatio)).RGBA(); r>>8 < 200 || g>>8 > 40 || b>>8 > 40 {
		t.Fatalf("annotation box was not drawn into the pixels: %d %d %d", r>>8, g>>8, b>>8)
	}
	if _, taskErr := session.ActVisual(ctx, pageID, VisualActionRequest{ObservationID: plain.ID, Kind: actions.KindClick, X: 1, Y: 1}); taskErr == nil || taskErr.Code != TaskErrorStaleReference {
		t.Fatalf("a superseded observation stayed actionable: %v", taskErr)
	}

	evaluate(t, ctx, session, pageID, "window.scrollTo(0, 40)")
	scrolled := observeVisual(t, ctx, session, pageID, VisualCaptureOptions{})
	if top := visualAnnotation(t, scrolled, "Alpha").Box; !near(top.Y, 0) || !near(top.Height, 20) || scrolled.Viewport.PageY != 40 {
		t.Fatalf("scrolled annotation is not clipped to the viewport: %+v %+v", top, scrolled.Viewport)
	}
	evaluate(t, ctx, session, pageID, "window.scrollTo(0, 400)")
	if gone := observeVisual(t, ctx, session, pageID, VisualCaptureOptions{}); len(gone.Annotations) != 0 {
		t.Fatalf("scrolled-out references were annotated: %+v", gone.Annotations)
	}
	evaluate(t, ctx, session, pageID, "window.scrollTo(0, 0)")
	cropped := observeVisual(t, ctx, session, pageID, VisualCaptureOptions{CropRef: alpha.Ref})
	if cropped.Crop == nil || !near(cropped.Crop.X, 20) || !near(cropped.Crop.Width, 120) || !near(float64(cropped.PixelWidth)/cropped.PixelRatio, 120) || len(cropped.Annotations) != 1 || cropped.Annotations[0].Name != "Alpha" || !near(cropped.Annotations[0].Box.X, 0) {
		t.Fatalf("crop is not bound to the element: %+v", cropped)
	}
	if _, taskErr := session.CaptureVisual(ctx, pageID, VisualCaptureOptions{MaxAnnotations: 500}); taskErr == nil || taskErr.Code != TaskErrorInvalidInput {
		t.Fatalf("unbounded annotation request accepted: %v", taskErr)
	}
	if _, taskErr := session.CaptureVisual(ctx, pageID, VisualCaptureOptions{CropRef: "other:e1"}); taskErr == nil {
		t.Fatal("foreign crop reference accepted")
	}
}

func TestVisualActionPrefersReferencesAndRefusesStaleOrCoveredPoints(t *testing.T) {
	session, pageID, ctx := startVisualSession(t, nil)
	observation := observeVisual(t, ctx, session, pageID, VisualCaptureOptions{})
	alpha := visualAnnotation(t, observation, "Alpha")
	x, y := center(alpha)
	result, taskErr := session.ActVisual(ctx, pageID, VisualActionRequest{ObservationID: observation.ID, Kind: actions.KindClick, X: x, Y: y})
	if taskErr != nil || result.Path != VisualPathReference || result.Ref != alpha.Ref || !result.Outcome.Success || !result.SemanticChanged || !strings.Contains(pageText(t, session, pageID), "clicked alpha") {
		t.Fatalf("grounded click did not run through the reference: %+v %v", result, taskErr)
	}
	if _, taskErr := session.ActVisual(ctx, pageID, VisualActionRequest{ObservationID: observation.ID, Kind: actions.KindClick, X: x, Y: y}); taskErr == nil || taskErr.Code != TaskErrorStaleReference {
		t.Fatalf("a consumed observation was reused: %v", taskErr)
	}

	stale := []struct {
		name   string
		mutate string
		reason string
	}{
		{"scrolled viewport", "window.scrollTo(0, 40)", "viewport"},
		{"changed pixels", "document.getElementById('beta').style.background = '#c0c'", "pixels"},
		{"scrolled frame", "document.querySelector('iframe').contentWindow.scrollTo(0, 80)", "pixels"},
		{"navigated document", "", "document"},
	}
	for _, step := range stale {
		evaluate(t, ctx, session, pageID, "window.scrollTo(0, 0)")
		observation = observeVisual(t, ctx, session, pageID, VisualCaptureOptions{})
		target := visualAnnotation(t, observation, "Beta")
		x, y = center(target)
		if step.name == "scrolled frame" {
			x, y = 380, 150
		}
		if step.mutate == "" {
			reload(t, session, pageID, observation.URL+"?again")
		} else {
			evaluate(t, ctx, session, pageID, step.mutate)
		}
		if _, taskErr := session.ActVisual(ctx, pageID, VisualActionRequest{ObservationID: observation.ID, Kind: actions.KindClick, X: x, Y: y, AllowPointerFallback: true}); taskErr == nil || taskErr.Code != TaskErrorStaleReference || !strings.Contains(taskErr.Error(), step.reason) {
			t.Fatalf("%s: stale point was dispatched or refused for the wrong reason: %v", step.name, taskErr)
		}
	}
	if strings.Contains(pageText(t, session, pageID), "clicked beta") {
		t.Fatal("a stale visual action reached the page")
	}

	reload(t, session, pageID, observation.URL)
	observation = observeVisual(t, ctx, session, pageID, VisualCaptureOptions{})
	beta := visualAnnotation(t, observation, "Beta")
	x, y = center(beta)
	// A transparent overlay changes no pixel but takes the hit: the point
	// no longer identifies the annotated target.
	evaluate(t, ctx, session, pageID, "const c=document.createElement('div');c.id='cover';c.style.cssText='position:absolute;left:190px;top:10px;width:150px;height:60px';document.body.appendChild(c)")
	if _, taskErr := session.ActVisual(ctx, pageID, VisualActionRequest{ObservationID: observation.ID, Kind: actions.KindClick, X: x, Y: y, AllowPointerFallback: true}); taskErr == nil || taskErr.Code != TaskErrorInvalidInput || !strings.Contains(taskErr.Error(), "covers") {
		t.Fatalf("covered target was dispatched: %v", taskErr)
	}
	if strings.Contains(pageText(t, session, pageID), "clicked beta") {
		t.Fatal("covered target was clicked")
	}

	observation = observeVisual(t, ctx, session, pageID, VisualCaptureOptions{})
	if _, taskErr := session.ActVisual(ctx, pageID, VisualActionRequest{ObservationID: observation.ID, Kind: actions.KindClick, X: observation.Viewport.Width + 5, Y: 5}); taskErr == nil || taskErr.Code != TaskErrorInvalidInput {
		t.Fatalf("point outside the image accepted: %v", taskErr)
	}
	if _, taskErr := session.ActVisual(ctx, pageID, VisualActionRequest{ObservationID: observation.ID, Kind: actions.KindFill, X: 5, Y: 5}); taskErr == nil || taskErr.Code != TaskErrorInvalidInput {
		t.Fatalf("non-pointer visual action accepted: %v", taskErr)
	}
	if _, taskErr := session.ActChromiumPage(ctx, pageID, 0, actions.Request{Kind: actions.KindClick, Pointer: &actions.PointerTarget{X: 30, Y: 30}}); taskErr == nil || taskErr.Code != TaskErrorInvalidInput {
		t.Fatalf("unbound pointer action accepted: %v", taskErr)
	}
}

func TestVisualPointerFallbackNeedsExplicitPermissionAndReportsBothChanges(t *testing.T) {
	session, pageID, ctx := startVisualSession(t, nil)
	observation := observeVisual(t, ctx, session, pageID, VisualCaptureOptions{})
	if _, taskErr := session.ActVisual(ctx, pageID, VisualActionRequest{ObservationID: observation.ID, Kind: actions.KindClick, X: 70, Y: 150}); taskErr == nil || taskErr.Code != TaskErrorInvalidInput || !strings.Contains(taskErr.Error(), "allowPointerFallback") {
		t.Fatalf("canvas point dispatched without explicit fallback: %v", taskErr)
	}
	if strings.Contains(pageText(t, session, pageID), "canvas") {
		t.Fatal("refused fallback reached the canvas")
	}
	result, taskErr := session.ActVisual(ctx, pageID, VisualActionRequest{ObservationID: observation.ID, Kind: actions.KindClick, X: 70, Y: 150, AllowPointerFallback: true})
	if taskErr != nil || result.Path != VisualPathPointer || result.Ref != "" || !result.Outcome.Success || result.Outcome.Evidence.Effect != actions.EffectDispatched {
		t.Fatalf("explicit pointer fallback failed: %+v %v", result, taskErr)
	}
	if text := pageText(t, session, pageID); !strings.Contains(text, "canvas 50,30") {
		t.Fatalf("pointer did not reach the canvas point: %q", text)
	}
	change := result.ImageChange
	if !change.Changed() || change.Bounds == nil || change.Ratio <= 0 || change.Ratio > 0.2 || change.Cells > 128*128 {
		t.Fatalf("bounded image change missing: %+v", change)
	}
	ratio := observation.PixelRatio
	if !change.Bounds.Overlaps(imageRect(55*ratio, 135*ratio, 85*ratio, 165*ratio)) {
		t.Fatalf("image change does not locate the painted square: %v", change.Bounds)
	}
	if !result.SemanticChanged {
		t.Fatal("semantic AX change of the log text was not reported")
	}

	// An inaccessible control (a div with a click handler and no role) has
	// no reference either: the same explicit fallback is required.
	observation = observeVisual(t, ctx, session, pageID, VisualCaptureOptions{})
	for _, annotation := range observation.Annotations {
		if annotation.Name == "Fake" {
			t.Fatalf("inaccessible control was annotated as a reference: %+v", annotation)
		}
	}
	if _, taskErr := session.ActVisual(ctx, pageID, VisualActionRequest{ObservationID: observation.ID, Kind: actions.KindClick, X: 200, Y: 315}); taskErr == nil || taskErr.Code != TaskErrorInvalidInput {
		t.Fatalf("inaccessible control clicked without explicit fallback: %v", taskErr)
	}
	fake, taskErr := session.ActVisual(ctx, pageID, VisualActionRequest{ObservationID: observation.ID, Kind: actions.KindClick, X: 200, Y: 315, AllowPointerFallback: true})
	if taskErr != nil || fake.Path != VisualPathPointer || !strings.Contains(pageText(t, session, pageID), "fake control") {
		t.Fatalf("explicit fallback did not reach the inaccessible control: %+v %v", fake, taskErr)
	}
}

type annotationGrounder struct {
	name string
	off  bool
}

func (g annotationGrounder) Ground(_ context.Context, request VisualGroundingRequest) (VisualPoint, error) {
	if g.off {
		return VisualPoint{X: request.Width + 50, Y: 10, Confidence: 0.9}, nil
	}
	if _, err := png.DecodeConfig(bytes.NewReader(request.Image)); err != nil || request.Instruction == "" {
		return VisualPoint{}, fmt.Errorf("grounder received no image or instruction: %v", err)
	}
	for _, annotation := range request.Annotations {
		if annotation.Name == g.name {
			x, y := center(annotation)
			return VisualPoint{X: x, Y: y, Confidence: 0.8}, nil
		}
	}
	return VisualPoint{}, fmt.Errorf("no %s in view", g.name)
}

func TestGroundVisualUsesOnlyTheInjectedModelCapability(t *testing.T) {
	session, pageID, ctx := startVisualSession(t, nil)
	observation := observeVisual(t, ctx, session, pageID, VisualCaptureOptions{})
	if _, taskErr := session.GroundVisual(ctx, pageID, observation.ID, "the beta button"); taskErr == nil || taskErr.Code != TaskErrorCapabilityUnavailable {
		t.Fatalf("grounding without a model capability: %v", taskErr)
	}

	grounded, groundedPage, groundedCtx := startVisualSession(t, annotationGrounder{name: "Beta"})
	observation = observeVisual(t, groundedCtx, grounded, groundedPage, VisualCaptureOptions{Annotate: true})
	point, taskErr := grounded.GroundVisual(groundedCtx, groundedPage, observation.ID, "the beta button")
	if taskErr != nil || point.ObservationID != observation.ID || point.Confidence != 0.8 {
		t.Fatalf("grounding failed: %+v %v", point, taskErr)
	}
	result, taskErr := grounded.ActVisual(groundedCtx, groundedPage, VisualActionRequest{ObservationID: point.ObservationID, Kind: actions.KindClick, X: point.X, Y: point.Y})
	if taskErr != nil || result.Path != VisualPathReference || !strings.Contains(pageText(t, grounded, groundedPage), "clicked beta") {
		t.Fatalf("grounded point did not act through the reference: %+v %v", result, taskErr)
	}
	if _, taskErr := grounded.GroundVisual(groundedCtx, groundedPage, observation.ID, "the beta button"); taskErr == nil || taskErr.Code != TaskErrorStaleReference {
		t.Fatalf("grounding against a consumed observation: %v", taskErr)
	}

	off, offPage, offCtx := startVisualSession(t, annotationGrounder{off: true})
	observation = observeVisual(t, offCtx, off, offPage, VisualCaptureOptions{})
	if _, taskErr := off.GroundVisual(offCtx, offPage, observation.ID, "anything"); taskErr == nil || taskErr.Code != TaskErrorExecutionFailed {
		t.Fatalf("ungrounded model point accepted: %v", taskErr)
	}
}

func imageRect(x0, y0, x1, y1 float64) image.Rectangle {
	return image.Rect(int(x0), int(y0), int(x1), int(y1))
}
