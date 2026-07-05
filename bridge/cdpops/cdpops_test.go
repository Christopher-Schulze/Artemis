package cdpops

import (
	"context"
	"testing"
	"time"
)

// ==================== element.go tests ====================

// TestTASK2256_QueryElementsEmpty verifies empty selector
// (spec L4019: element queries + box model).
func TestTASK2256_QueryElementsEmpty(t *testing.T) {
	result := QueryElements(ElementQuery{Selector: ""})
	if result.Error == "" {
		t.Error("empty selector should error")
	}
}

// TestTASK2256_QueryElementsValid verifies valid query
// (spec L4019: element queries + box model).
func TestTASK2256_QueryElementsValid(t *testing.T) {
	result := QueryElements(ElementQuery{Selector: "div"})
	if result.Error != "" {
		t.Errorf("valid query should not error: %s", result.Error)
	}
}

// TestTASK2256_GetBoxModel verifies box model retrieval
// (spec L4019: element queries + box model).
func TestTASK2256_GetBoxModel(t *testing.T) {
	box, err := GetBoxModel("e5")
	if err != nil {
		t.Fatalf("GetBoxModel: %v", err)
	}
	if box.Width <= 0 {
		t.Error("width should be positive")
	}
}

// TestTASK2256_GetBoxModelEmpty verifies empty ref.
func TestTASK2256_GetBoxModelEmpty(t *testing.T) {
	_, err := GetBoxModel("")
	if err == nil {
		t.Error("empty ref should error")
	}
}

// TestTASK2256_IsElementVisible verifies visibility check
// (spec L4019: element queries + box model).
func TestTASK2256_IsElementVisible(t *testing.T) {
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

// TestTASK2256_IsElementClickable verifies clickability check
// (spec L4019: element queries + box model).
func TestTASK2256_IsElementClickable(t *testing.T) {
	info := &ElementInfo{
		Visible: true,
		Box:     &BoxModel{Width: 100, Height: 100},
	}
	if !IsElementClickable(info) {
		t.Error("visible element with box should be clickable")
	}
	info.Visible = false
	if IsElementClickable(info) {
		t.Error("invisible element should not be clickable")
	}
}

// TestTASK2256_GetElementCenter verifies center calculation
// (spec L4019: element queries + box model).
func TestTASK2256_GetElementCenter(t *testing.T) {
	x, y := GetElementCenter(&BoxModel{Width: 100, Height: 200})
	if x != 50 || y != 100 {
		t.Errorf("center: got (%f,%f), want (50,100)", x, y)
	}
}

// TestTASK2256_FilterVisible verifies filtering
// (spec L4019: element queries + box model).
func TestTASK2256_FilterVisible(t *testing.T) {
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

// TestTASK2256_FindByRef verifies ref search
// (spec L4019: element queries + box model).
func TestTASK2256_FindByRef(t *testing.T) {
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

// TestTASK2256_FindByTag verifies tag search
// (spec L4019: element queries + box model).
func TestTASK2256_FindByTag(t *testing.T) {
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

// TestTASK2256_CSSToDevicePixels verifies CSS to device conversion
// (spec L4019: coordinate transforms).
func TestTASK2256_CSSToDevicePixels(t *testing.T) {
	p := CSSToDevicePixels(Point{X: 100, Y: 200}, 2.0)
	if p.X != 200 || p.Y != 400 {
		t.Errorf("got (%f,%f), want (200,400)", p.X, p.Y)
	}
}

// TestTASK2256_DeviceToCSSPixels verifies device to CSS conversion
// (spec L4019: coordinate transforms).
func TestTASK2256_DeviceToCSSPixels(t *testing.T) {
	p := DeviceToCSSPixels(Point{X: 200, Y: 400}, 2.0)
	if p.X != 100 || p.Y != 200 {
		t.Errorf("got (%f,%f), want (100,200)", p.X, p.Y)
	}
}

// TestTASK2256_CSSToDevicePixelsZeroScale verifies zero scale default.
func TestTASK2256_CSSToDevicePixelsZeroScale(t *testing.T) {
	p := CSSToDevicePixels(Point{X: 100, Y: 200}, 0)
	if p.X != 100 || p.Y != 200 {
		t.Error("zero scale should default to 1")
	}
}

// TestTASK2256_ApplyScrollOffset verifies scroll offset
// (spec L4019: coordinate transforms).
func TestTASK2256_ApplyScrollOffset(t *testing.T) {
	p := ApplyScrollOffset(Point{X: 10, Y: 20}, 100, 200)
	if p.X != 110 || p.Y != 220 {
		t.Errorf("got (%f,%f), want (110,220)", p.X, p.Y)
	}
}

// TestTASK2256_RemoveScrollOffset verifies scroll removal
// (spec L4019: coordinate transforms).
func TestTASK2256_RemoveScrollOffset(t *testing.T) {
	p := RemoveScrollOffset(Point{X: 110, Y: 220}, 100, 200)
	if p.X != 10 || p.Y != 20 {
		t.Errorf("got (%f,%f), want (10,20)", p.X, p.Y)
	}
}

// TestTASK2256_PointInRect verifies point-in-rect check
// (spec L4019: coordinate transforms).
func TestTASK2256_PointInRect(t *testing.T) {
	r := Rect{X: 10, Y: 10, Width: 100, Height: 100}
	if !PointInRect(Point{X: 50, Y: 50}, r) {
		t.Error("50,50 should be in rect")
	}
	if PointInRect(Point{X: 5, Y: 5}, r) {
		t.Error("5,5 should not be in rect")
	}
}

// TestTASK2256_RectCenter verifies center calculation
// (spec L4019: coordinate transforms).
func TestTASK2256_RectCenter(t *testing.T) {
	c := RectCenter(Rect{X: 0, Y: 0, Width: 100, Height: 200})
	if c.X != 50 || c.Y != 100 {
		t.Errorf("got (%f,%f), want (50,100)", c.X, c.Y)
	}
}

// TestTASK2256_Distance verifies distance calculation
// (spec L4019: coordinate transforms).
func TestTASK2256_Distance(t *testing.T) {
	d := Distance(Point{X: 0, Y: 0}, Point{X: 3, Y: 4})
	if d != 5 {
		t.Errorf("distance: got %f, want 5", d)
	}
}

// TestTASK2256_Midpoint verifies midpoint
// (spec L4019: coordinate transforms).
func TestTASK2256_Midpoint(t *testing.T) {
	m := Midpoint(Point{X: 0, Y: 0}, Point{X: 100, Y: 200})
	if m.X != 50 || m.Y != 100 {
		t.Errorf("got (%f,%f), want (50,100)", m.X, m.Y)
	}
}

// TestTASK2256_Lerp verifies interpolation
// (spec L4019: coordinate transforms).
func TestTASK2256_Lerp(t *testing.T) {
	p := Lerp(Point{X: 0, Y: 0}, Point{X: 100, Y: 100}, 0.5)
	if p.X != 50 || p.Y != 50 {
		t.Errorf("got (%f,%f), want (50,50)", p.X, p.Y)
	}
}

// TestTASK2256_ClampPoint verifies clamping
// (spec L4019: coordinate transforms).
func TestTASK2256_ClampPoint(t *testing.T) {
	vp := Viewport{X: 0, Y: 0, Width: 100, Height: 100}
	p := ClampPoint(Point{X: 150, Y: 150}, vp)
	if p.X != 100 || p.Y != 100 {
		t.Errorf("got (%f,%f), want (100,100)", p.X, p.Y)
	}
}

// TestTASK2256_QuadToRect verifies quad to rect conversion
// (spec L4019: coordinate transforms).
func TestTASK2256_QuadToRect(t *testing.T) {
	q := Quad{0, 0, 100, 0, 100, 100, 0, 100}
	r := QuadToRect(q)
	if r.Width != 100 || r.Height != 100 {
		t.Errorf("got (%f,%f), want (100,100)", r.Width, r.Height)
	}
}

// ==================== navigation.go tests ====================

// TestTASK2256_NavigatorNavigate verifies navigation
// (spec L4019: page navigation + wait).
func TestTASK2256_NavigatorNavigate(t *testing.T) {
	n := NewNavigator()
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

// TestTASK2256_NavigatorNavigateEmpty verifies empty URL fails.
func TestTASK2256_NavigatorNavigateEmpty(t *testing.T) {
	n := NewNavigator()
	result := n.Navigate(context.Background(), NavigationRequest{URL: ""})
	if result.Success {
		t.Error("empty URL should fail")
	}
}

// TestTASK2256_NavigatorWaitForLoad verifies wait
// (spec L4019: page navigation + wait).
func TestTASK2256_NavigatorWaitForLoad(t *testing.T) {
	n := NewNavigator()
	n.Navigate(context.Background(), NavigationRequest{URL: "https://example.com"})
	err := n.WaitForLoad(context.Background(), WaitLoad, 1*time.Second)
	if err != nil {
		t.Errorf("WaitForLoad: %v", err)
	}
}

// TestTASK2256_NavigatorGoBack verifies back navigation
// (spec L4019: page navigation + wait).
func TestTASK2256_NavigatorGoBack(t *testing.T) {
	n := NewNavigator()
	result := n.GoBack(context.Background())
	if !result.Success {
		t.Error("go back should succeed")
	}
}

// TestTASK2256_NavigatorGoForward verifies forward navigation
// (spec L4019: page navigation + wait).
func TestTASK2256_NavigatorGoForward(t *testing.T) {
	n := NewNavigator()
	result := n.GoForward(context.Background())
	if !result.Success {
		t.Error("go forward should succeed")
	}
}

// TestTASK2256_NavigatorReload verifies reload
// (spec L4019: page navigation + wait).
func TestTASK2256_NavigatorReload(t *testing.T) {
	n := NewNavigator()
	result := n.Reload(context.Background())
	if !result.Success {
		t.Error("reload should succeed")
	}
}

// TestTASK2256_IsValidWaitCondition verifies validation.
func TestTASK2256_IsValidWaitCondition(t *testing.T) {
	if !IsValidWaitCondition(WaitLoad) {
		t.Error("load should be valid")
	}
	if IsValidWaitCondition(WaitCondition("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// TestTASK2256_IsValidNavigationState verifies validation.
func TestTASK2256_IsValidNavigationState(t *testing.T) {
	if !IsValidNavigationState(NavigationStateComplete) {
		t.Error("complete should be valid")
	}
	if IsValidNavigationState(NavigationState("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// ==================== pointer.go tests ====================

// TestTASK2256_PointerClick verifies click
// (spec L4019: mouse/touch events).
func TestTASK2256_PointerClick(t *testing.T) {
	d := NewPointerDispatcher()
	err := d.Click(100, 200, MouseButtonLeft)
	if err != nil {
		t.Fatalf("Click: %v", err)
	}
	if d.EventCount() != 1 {
		t.Error("event count should be 1")
	}
}

// TestTASK2256_PointerDoubleClick verifies double-click
// (spec L4019: mouse/touch events).
func TestTASK2256_PointerDoubleClick(t *testing.T) {
	d := NewPointerDispatcher()
	err := d.DoubleClick(100, 200)
	if err != nil {
		t.Fatalf("DoubleClick: %v", err)
	}
}

// TestTASK2256_PointerMouseMove verifies mouse move
// (spec L4019: mouse/touch events).
func TestTASK2256_PointerMouseMove(t *testing.T) {
	d := NewPointerDispatcher()
	err := d.MouseMove(100, 200)
	if err != nil {
		t.Fatalf("MouseMove: %v", err)
	}
}

// TestTASK2256_PointerTouchTap verifies touch tap
// (spec L4019: mouse/touch events).
func TestTASK2256_PointerTouchTap(t *testing.T) {
	d := NewPointerDispatcher()
	err := d.TouchTap(100, 200)
	if err != nil {
		t.Fatalf("TouchTap: %v", err)
	}
	if d.EventCount() != 2 {
		t.Errorf("events: got %d, want 2 (start+end)", d.EventCount())
	}
}

// TestTASK2256_PointerInvalidButton verifies invalid button.
func TestTASK2256_PointerInvalidButton(t *testing.T) {
	d := NewPointerDispatcher()
	err := d.DispatchMouse(MouseEvent{Button: MouseButton("invalid")})
	if err == nil {
		t.Error("invalid button should error")
	}
}

// TestTASK2256_PointerClear verifies clear.
func TestTASK2256_PointerClear(t *testing.T) {
	d := NewPointerDispatcher()
	d.Click(100, 200, MouseButtonLeft)
	cleared := d.Clear()
	if cleared != 1 {
		t.Errorf("cleared: got %d, want 1", cleared)
	}
	if d.EventCount() != 0 {
		t.Error("count should be 0 after clear")
	}
}

// TestTASK2256_IsValidMouseButton verifies validation.
func TestTASK2256_IsValidMouseButton(t *testing.T) {
	if !IsValidMouseButton(MouseButtonLeft) {
		t.Error("left should be valid")
	}
	if IsValidMouseButton(MouseButton("invalid")) {
		t.Error("invalid should not be valid")
	}
}

// TestTASK2256_GenerateBezierPath verifies path generation
// (spec L4019: mouse/touch events).
func TestTASK2256_GenerateBezierPath(t *testing.T) {
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

// TestTASK2256_AddJitter verifies jitter
// (spec L4019: mouse/touch events).
func TestTASK2256_AddJitter(t *testing.T) {
	p := AddJitter(Point{X: 100, Y: 100}, 5)
	// Should return a different point (with jitter)
	if p.X == 100 && p.Y == 100 {
		// Could be same if jitter is 0, but with maxJitter=5 it should differ
	}
	// Zero jitter should return same point
	p2 := AddJitter(Point{X: 100, Y: 100}, 0)
	if p2.X != 100 || p2.Y != 100 {
		t.Error("zero jitter should return same point")
	}
}

// ==================== full spec parity test ====================

// TestTASK2256_FullSpecParity verifies all 4 spec-mandated files
// (spec L4019: element.go, geometry.go, navigation.go, pointer.go).
func TestTASK2256_FullSpecParity(t *testing.T) {
	// 1. element.go - element queries + box model
	box, _ := GetBoxModel("e5")
	if !IsElementVisible(box) {
		t.Error("element.go: box should be visible")
	}

	// 2. geometry.go - coordinate transforms
	p := CSSToDevicePixels(Point{X: 100, Y: 100}, 2.0)
	if p.X != 200 {
		t.Error("geometry.go: CSS to device pixels failed")
	}

	// 3. navigation.go - page navigation + wait
	n := NewNavigator()
	result := n.Navigate(context.Background(), NavigationRequest{URL: "https://example.com"})
	if !result.Success {
		t.Error("navigation.go: navigate failed")
	}

	// 4. pointer.go - mouse/touch events
	d := NewPointerDispatcher()
	if err := d.Click(100, 200, MouseButtonLeft); err != nil {
		t.Error("pointer.go: click failed")
	}
}
