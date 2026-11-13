package cdpops

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

type cdpopsTestCaller struct {
	calls      []string
	failMethod string
}

func (c *cdpopsTestCaller) Call(_ context.Context, method string, _ any, result any) error {
	c.calls = append(c.calls, method)
	if method == c.failMethod {
		return fmt.Errorf("forced CDP failure for %s", method)
	}
	var raw []byte
	switch method {
	case "Page.navigate":
		raw = []byte(`{"frameId":"frame-1","loaderId":"loader-1"}`)
	case "Runtime.evaluate":
		raw = []byte(`{"result":{"value":"complete"}}`)
	case "Page.getNavigationHistory":
		raw = []byte(`{"currentIndex":1,"entries":[{"id":1,"url":"https://one.example"},{"id":2,"url":"https://two.example"},{"id":3,"url":"https://three.example"}]}`)
	case "Page.navigateToHistoryEntry", "Page.reload", "Input.dispatchMouseEvent", "Input.dispatchTouchEvent":
		raw = []byte(`{}`)
	default:
		return fmt.Errorf("unexpected CDP method %s", method)
	}
	return json.Unmarshal(raw, result)
}

func newCDPOpsTestCaller() *cdpopsTestCaller { return &cdpopsTestCaller{} }

func TestCDPPrimitivesRequireProtocolAndCaller(t *testing.T) {
	noCaller := NewNavigator()
	if result := noCaller.Navigate(context.Background(), NavigationRequest{URL: "https://example.com"}); result.Success {
		t.Fatal("navigator reported success without a CDP caller")
	}
	caller := newCDPOpsTestCaller()
	dispatcher := NewPointerDispatcher(caller)
	if err := dispatcher.ClickContext(context.Background(), 10, 20, MouseButtonLeft); err != nil {
		t.Fatal(err)
	}
	if dispatcher.EventCount() != 2 {
		t.Fatalf("event count=%d, want press+release", dispatcher.EventCount())
	}
	for _, want := range []string{"Input.dispatchMouseEvent", "Input.dispatchMouseEvent"} {
		if len(caller.calls) == 0 || caller.calls[len(caller.calls)-1] != want {
			t.Fatalf("last CDP call=%v, want %s", caller.calls, want)
		}
		caller.calls = caller.calls[:len(caller.calls)-1]
	}
}

func TestCDPFailureCannotBecomeSuccessOrHistory(t *testing.T) {
	navigator := NewNavigator(&cdpopsTestCaller{failMethod: "Page.navigate"})
	if result := navigator.Navigate(context.Background(), NavigationRequest{URL: "https://example.com"}); result.Success {
		t.Fatal("navigation converted a CDP failure into success")
	}
	caller := &cdpopsTestCaller{failMethod: "Input.dispatchMouseEvent"}
	dispatcher := NewPointerDispatcher(caller)
	if err := dispatcher.Click(10, 20, MouseButtonLeft); err == nil {
		t.Fatal("pointer swallowed a CDP failure")
	}
	if dispatcher.EventCount() != 0 {
		t.Fatal("failed pointer event was recorded as executed")
	}
}

// ==================== element.go tests ====================

// TestQueryElementsEmpty verifies empty selector
// (element queries + box model).
func TestQueryElementsEmpty(t *testing.T) {
	result := QueryElements(ElementQuery{Selector: ""})
	if result.Error == "" {
		t.Error("empty selector should error")
	}
}

// TestQueryElementsValid verifies valid query
// (element queries + box model).
func TestQueryElementsValid(t *testing.T) {
	result := QueryElements(ElementQuery{Selector: "div"})
	if result.Error == "" {
		t.Error("legacy query without a CDP caller must fail explicitly")
	}
}

// TestGetBoxModel verifies box model retrieval
// (element queries + box model).
func TestGetBoxModel(t *testing.T) {
	box, err := GetBoxModel("e5")
	if err == nil || box != nil {
		t.Fatal("legacy fixed geometry must not be returned")
	}
}

// TestGetBoxModelEmpty verifies empty ref.
func TestGetBoxModelEmpty(t *testing.T) {
	_, err := GetBoxModel("")
	if err == nil {
		t.Error("empty ref should error")
	}
}

// TestIsElementVisible verifies visibility check
// (element queries + box model).
func TestIsElementVisible(t *testing.T) {
	if !IsElementVisible(&BoxModel{Width: 100, Height: 100}) {
		t.Error("100x100 should be visible")
	}
	if IsElementVisible(&BoxModel{Width: 0, Height: 0}) {
		t.Error("0x0 should not be visible")
	}
	if IsElementVisible(nil) {
		t.Error("nil should not be visible")
	}
}

// TestIsElementClickable verifies clickability check
// (element queries + box model).
func TestIsElementClickable(t *testing.T) {
	info := &ElementInfo{
		Attachment:    ElementAttached,
		Layout:        ElementLayoutPresent,
		Visible:       true,
		Interactive:   true,
		Hit:           HitClear,
		Actionability: ActionabilityReady,
		Box:           &BoxModel{Width: 100, Height: 100},
	}
	if !IsElementClickable(info) {
		t.Error("fully evidenced actionable element should be clickable")
	}
	info.Hit = HitUnknown
	if IsElementClickable(info) {
		t.Error("box geometry without a clear hit test should not be clickable")
	}
}

// TestGetElementCenter verifies center calculation
// (element queries + box model).
func TestGetElementCenter(t *testing.T) {
	x, y := GetElementCenter(&BoxModel{Width: 100, Height: 200})
	if x != 50 || y != 100 {
		t.Errorf("center: got (%f,%f), want (50,100)", x, y)
	}
}

// TestFilterVisible verifies filtering
// (element queries + box model).
func TestFilterVisible(t *testing.T) {
	elements := []ElementInfo{
		{Ref: "e1", Visible: true},
		{Ref: "e2", Visible: false},
		{Ref: "e3", Visible: true},
	}
	visible := FilterVisible(elements)
	if len(visible) != 2 {
		t.Errorf("visible: got %d, want 2", len(visible))
	}
}

// TestFindByRef verifies ref search
// (element queries + box model).
func TestFindByRef(t *testing.T) {
	elements := []ElementInfo{
		{Ref: "e1", TagName: "div"},
		{Ref: "e2", TagName: "span"},
	}
	elem, ok := FindByRef(elements, "e2")
	if !ok || elem.TagName != "span" {
		t.Error("should find e2")
	}
	_, ok = FindByRef(elements, "e99")
	if ok {
		t.Error("should not find e99")
	}
}

// TestFindByTag verifies tag search
// (element queries + box model).
func TestFindByTag(t *testing.T) {
	elements := []ElementInfo{
		{Ref: "e1", TagName: "div"},
		{Ref: "e2", TagName: "DIV"},
		{Ref: "e3", TagName: "span"},
	}
	divs := FindByTag(elements, "div")
	if len(divs) != 2 {
		t.Errorf("divs: got %d, want 2 (case-insensitive)", len(divs))
	}
}

// ==================== geometry.go tests ====================

// TestCSSToDevicePixels verifies CSS to device conversion
// (coordinate transforms).
func TestCSSToDevicePixels(t *testing.T) {
	p := CSSToDevicePixels(Point{X: 100, Y: 200}, 2.0)
	if p.X != 200 || p.Y != 400 {
		t.Errorf("got (%f,%f), want (200,400)", p.X, p.Y)
	}
}

// TestDeviceToCSSPixels verifies device to CSS conversion
// (coordinate transforms).
func TestDeviceToCSSPixels(t *testing.T) {
	p := DeviceToCSSPixels(Point{X: 200, Y: 400}, 2.0)
	if p.X != 100 || p.Y != 200 {
		t.Errorf("got (%f,%f), want (100,200)", p.X, p.Y)
	}
}

// TestCSSToDevicePixelsZeroScale verifies zero scale default.
func TestCSSToDevicePixelsZeroScale(t *testing.T) {
	p := CSSToDevicePixels(Point{X: 100, Y: 200}, 0)
	if p.X != 100 || p.Y != 200 {
		t.Error("zero scale should default to 1")
	}
}

// TestApplyScrollOffset verifies scroll offset
// (coordinate transforms).
func TestApplyScrollOffset(t *testing.T) {
	p := ApplyScrollOffset(Point{X: 10, Y: 20}, 100, 200)
	if p.X != 110 || p.Y != 220 {
		t.Errorf("got (%f,%f), want (110,220)", p.X, p.Y)
	}
}

// TestRemoveScrollOffset verifies scroll removal
// (coordinate transforms).
func TestRemoveScrollOffset(t *testing.T) {
	p := RemoveScrollOffset(Point{X: 110, Y: 220}, 100, 200)
	if p.X != 10 || p.Y != 20 {
		t.Errorf("got (%f,%f), want (10,20)", p.X, p.Y)
	}
}

// TestPointInRect verifies point-in-rect check
// (coordinate transforms).
func TestPointInRect(t *testing.T) {
	r := Rect{X: 10, Y: 10, Width: 100, Height: 100}
	if !PointInRect(Point{X: 50, Y: 50}, r) {
		t.Error("50,50 should be in rect")
	}
	if PointInRect(Point{X: 5, Y: 5}, r) {
		t.Error("5,5 should not be in rect")
	}
}

// TestRectCenter verifies center calculation
// (coordinate transforms).
func TestRectCenter(t *testing.T) {
	c := RectCenter(Rect{X: 0, Y: 0, Width: 100, Height: 200})
	if c.X != 50 || c.Y != 100 {
		t.Errorf("got (%f,%f), want (50,100)", c.X, c.Y)
	}
}

// TestDistance verifies distance calculation
// (coordinate transforms).
func TestDistance(t *testing.T) {
	d := Distance(Point{X: 0, Y: 0}, Point{X: 3, Y: 4})
	if d != 5 {
		t.Errorf("distance: got %f, want 5", d)
	}
}

// TestMidpoint verifies midpoint
// (coordinate transforms).
func TestMidpoint(t *testing.T) {
	m := Midpoint(Point{X: 0, Y: 0}, Point{X: 100, Y: 200})
	if m.X != 50 || m.Y != 100 {
		t.Errorf("got (%f,%f), want (50,100)", m.X, m.Y)
	}
}

// TestLerp verifies interpolation
// (coordinate transforms).
func TestLerp(t *testing.T) {
	p := Lerp(Point{X: 0, Y: 0}, Point{X: 100, Y: 100}, 0.5)
	if p.X != 50 || p.Y != 50 {
		t.Errorf("got (%f,%f), want (50,50)", p.X, p.Y)
	}
}

// TestClampPoint verifies clamping
// (coordinate transforms).
func TestClampPoint(t *testing.T) {
	vp := Viewport{X: 0, Y: 0, Width: 100, Height: 100}
	p := ClampPoint(Point{X: 150, Y: 150}, vp)
	if p.X != 100 || p.Y != 100 {
		t.Errorf("got (%f,%f), want (100,100)", p.X, p.Y)
	}
}

// TestQuadToRect verifies quad to rect conversion
// (coordinate transforms).
func TestQuadToRect(t *testing.T) {
	q := Quad{0, 0, 100, 0, 100, 100, 0, 100}
	r := QuadToRect(q)
	if r.Width != 100 || r.Height != 100 {
		t.Errorf("got (%f,%f), want (100,100)", r.Width, r.Height)
	}
}

// ==================== navigation.go tests ====================

// TestNavigatorNavigate verifies navigation
// (page navigation + wait).
func TestNavigatorNavigate(t *testing.T) {
	n := NewNavigator(newCDPOpsTestCaller())
	result := n.Navigate(context.Background(), NavigationRequest{
		URL:       "https://example.com",
		WaitUntil: WaitLoad,
		Timeout:   5 * time.Second,
	})
	if !result.Success {
		t.Error("navigation should succeed")
	}
	if n.State() != NavigationStateComplete {
		t.Error("state should be complete")
	}
	if n.CurrentURL() != "https://example.com" {
		t.Error("URL mismatch")
	}
}

// TestNavigatorNavigateEmpty verifies empty URL fails.
func TestNavigatorNavigateEmpty(t *testing.T) {
	n := NewNavigator()
	result := n.Navigate(context.Background(), NavigationRequest{URL: ""})
	if result.Success {
		t.Error("empty URL should fail")
	}
}

// TestNavigatorWaitForLoad verifies wait
// (page navigation + wait).
func TestNavigatorWaitForLoad(t *testing.T) {
	n := NewNavigator(newCDPOpsTestCaller())
	n.Navigate(context.Background(), NavigationRequest{URL: "https://example.com"})
	err := n.WaitForLoad(context.Background(), WaitLoad, 1*time.Second)
	if err != nil {
		t.Errorf("WaitForLoad: %v", err)
	}
}

// TestNavigatorGoBack verifies back navigation
// (page navigation + wait).
func TestNavigatorGoBack(t *testing.T) {
	n := NewNavigator(newCDPOpsTestCaller())
	result := n.GoBack(context.Background())
	if !result.Success {
		t.Error("go back should succeed")
	}
}

// TestNavigatorGoForward verifies forward navigation
// (page navigation + wait).
func TestNavigatorGoForward(t *testing.T) {
	n := NewNavigator(newCDPOpsTestCaller())
	result := n.GoForward(context.Background())
	if !result.Success {
		t.Error("go forward should succeed")
	}
}

// TestNavigatorReload verifies reload
// (page navigation + wait).
func TestNavigatorReload(t *testing.T) {
	n := NewNavigator(newCDPOpsTestCaller())
	result := n.Reload(context.Background())
	if !result.Success {
		t.Error("reload should succeed")
	}
}

// TestIsValidWaitCondition verifies validation.
func TestIsValidWaitCondition(t *testing.T) {
	if !IsValidWaitCondition(WaitLoad) {
		t.Error("load should be valid")
	}
	if IsValidWaitCondition(WaitCondition("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// TestIsValidNavigationState verifies validation.
func TestIsValidNavigationState(t *testing.T) {
	if !IsValidNavigationState(NavigationStateComplete) {
		t.Error("complete should be valid")
	}
	if IsValidNavigationState(NavigationState("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// ==================== pointer.go tests ====================

// TestPointerClick verifies click
// (mouse/touch events).
func TestPointerClick(t *testing.T) {
	d := NewPointerDispatcher(newCDPOpsTestCaller())
	err := d.Click(100, 200, MouseButtonLeft)
	if err != nil {
		t.Fatalf("Click: %v", err)
	}
	if d.EventCount() != 2 {
		t.Error("event count should be 2 (press+release)")
	}
}

// TestPointerDoubleClick verifies double-click
// (mouse/touch events).
func TestPointerDoubleClick(t *testing.T) {
	d := NewPointerDispatcher(newCDPOpsTestCaller())
	err := d.DoubleClick(100, 200)
	if err != nil {
		t.Fatalf("DoubleClick: %v", err)
	}
}

// TestPointerMouseMove verifies mouse move
// (mouse/touch events).
func TestPointerMouseMove(t *testing.T) {
	d := NewPointerDispatcher(newCDPOpsTestCaller())
	err := d.MouseMove(100, 200)
	if err != nil {
		t.Fatalf("MouseMove: %v", err)
	}
}

// TestPointerTouchTap verifies touch tap
// (mouse/touch events).
func TestPointerTouchTap(t *testing.T) {
	d := NewPointerDispatcher(newCDPOpsTestCaller())
	err := d.TouchTap(100, 200)
	if err != nil {
		t.Fatalf("TouchTap: %v", err)
	}
	if d.EventCount() != 2 {
		t.Errorf("events: got %d, want 2 (start+end)", d.EventCount())
	}
}

// TestPointerInvalidButton verifies invalid button.
func TestPointerInvalidButton(t *testing.T) {
	d := NewPointerDispatcher(newCDPOpsTestCaller())
	err := d.DispatchMouse(MouseEvent{Button: MouseButton("invalid")})
	if err == nil {
		t.Error("invalid button should error")
	}
}

// TestPointerClear verifies clear.
func TestPointerClear(t *testing.T) {
	d := NewPointerDispatcher(newCDPOpsTestCaller())
	if err := d.Click(100, 200, MouseButtonLeft); err != nil {
		t.Fatalf("Click: %v", err)
	}
	cleared := d.Clear()
	if cleared != 2 {
		t.Errorf("cleared: got %d, want 2", cleared)
	}
	if d.EventCount() != 0 {
		t.Error("count should be 0 after clear")
	}
}

// TestIsValidMouseButton verifies validation.
func TestIsValidMouseButton(t *testing.T) {
	if !IsValidMouseButton(MouseButtonLeft) {
		t.Error("left should be valid")
	}
	if IsValidMouseButton(MouseButton("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// TestGenerateBezierPath verifies path generation
// (mouse/touch events).
func TestGenerateBezierPath(t *testing.T) {
	path := GenerateBezierPath(Point{X: 0, Y: 0}, Point{X: 100, Y: 100}, 10)
	if len(path) != 11 {
		t.Errorf("path length: got %d, want 11", len(path))
	}
	// First point should be start
	if path[0].X != 0 || path[0].Y != 0 {
		t.Error("first point should be start")
	}
	// Last point should be end
	if path[10].X != 100 || path[10].Y != 100 {
		t.Error("last point should be end")
	}
}

// TestAddJitter verifies jitter
// (mouse/touch events).
func TestAddJitter(t *testing.T) {
	p := AddJitter(Point{X: 100, Y: 100}, 5)
	// Should return a different point (with jitter)
	if p.X == 100 && p.Y == 100 {
		t.Fatal("positive jitter did not change the point")
	}
	// Zero jitter should return same point
	p2 := AddJitter(Point{X: 100, Y: 100}, 0)
	if p2.X != 100 || p2.Y != 100 {
		t.Error("zero jitter should return same point")
	}
}

// ==================== parity test ====================

// TestCdpopsParity verifies all 4 spec-mandated files
// (element.go, geometry.go, navigation.go, pointer.go).
func TestCdpopsParity(t *testing.T) {
	// 1. element.go - element queries + box model
	box := &BoxModel{Width: 100, Height: 50}
	if !IsElementVisible(box) {
		t.Error("element.go: real box visibility failed")
	}

	// 2. geometry.go - coordinate transforms
	p := CSSToDevicePixels(Point{X: 100, Y: 100}, 2.0)
	if p.X != 200 {
		t.Error("geometry.go: CSS to device pixels failed")
	}

	// 3. navigation.go - page navigation + wait
	n := NewNavigator(newCDPOpsTestCaller())
	result := n.Navigate(context.Background(), NavigationRequest{URL: "https://example.com"})
	if !result.Success {
		t.Error("navigation.go: navigate failed")
	}

	// 4. pointer.go - mouse/touch events
	d := NewPointerDispatcher(newCDPOpsTestCaller())
	if err := d.Click(100, 200, MouseButtonLeft); err != nil {
		t.Error("pointer.go: click failed")
	}
}
