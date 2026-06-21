package bridge

import (
	"context"
	"testing"
	"time"
)

func TestFormActionsSetChecked(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	err := fa.SetChecked(context.Background(), "e5", true)
	if err != nil {
		t.Fatal(err)
	}
}

func TestFormActionsSetCheckedNoRef(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	err := fa.SetChecked(context.Background(), "", true)
	if err == nil {
		t.Fatal("expected error for empty ref")
	}
}

func TestFormActionsSetCheckedNoSession(t *testing.T) {
	fa := NewFormActions(nil)
	err := fa.SetChecked(context.Background(), "e5", true)
	if err == nil {
		t.Fatal("expected error for nil session")
	}
}

func TestFormActionsSelectOption(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	err := fa.SelectOption(context.Background(), "e3", "Option 1")
	if err != nil {
		t.Fatal(err)
	}
}

func TestFormActionsSelectOptionNoValue(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	err := fa.SelectOption(context.Background(), "e3", "")
	if err == nil {
		t.Fatal("expected error for empty value")
	}
}

func TestNetworkRequestBufferAddAndGet(t *testing.T) {
	buf := NewNetworkRequestBuffer(100)
	buf.AddRequest("page1", NetworkRequest{ID: "r1", Method: "GET", URL: "https://example.com"})
	if buf.Count("page1") != 1 {
		t.Fatalf("expected 1 request, got %d", buf.Count("page1"))
	}
	reqs := buf.GetRequests("page1")
	if len(reqs) != 1 || reqs[0].ID != "r1" {
		t.Fatalf("unexpected requests: %+v", reqs)
	}
}

func TestNetworkRequestBufferAddResponse(t *testing.T) {
	buf := NewNetworkRequestBuffer(100)
	buf.AddRequest("page1", NetworkRequest{ID: "r1", Method: "GET", URL: "https://example.com"})
	buf.AddResponse("page1", "r1", 200, true, "", 50*time.Millisecond)
	req, ok := buf.GetRequest("page1", "r1")
	if !ok {
		t.Fatal("expected request r1")
	}
	if req.Status != 200 {
		t.Fatalf("expected status 200, got %d", req.Status)
	}
	if !req.OK {
		t.Fatal("expected OK")
	}
	if req.ResponseTime != 50*time.Millisecond {
		t.Fatalf("expected 50ms, got %v", req.ResponseTime)
	}
}

func TestNetworkRequestBufferClear(t *testing.T) {
	buf := NewNetworkRequestBuffer(100)
	buf.AddRequest("page1", NetworkRequest{ID: "r1"})
	buf.AddRequest("page2", NetworkRequest{ID: "r2"})
	buf.Clear("page1")
	if buf.Count("page1") != 0 {
		t.Fatal("expected 0 after clear")
	}
	if buf.Count("page2") != 1 {
		t.Fatal("expected page2 to remain")
	}
}

func TestNetworkRequestBufferClearAll(t *testing.T) {
	buf := NewNetworkRequestBuffer(100)
	buf.AddRequest("page1", NetworkRequest{ID: "r1"})
	buf.AddRequest("page2", NetworkRequest{ID: "r2"})
	buf.ClearAll()
	if buf.Count("page1") != 0 || buf.Count("page2") != 0 {
		t.Fatal("expected all cleared")
	}
}

func TestNetworkRequestBufferMaxPer(t *testing.T) {
	buf := NewNetworkRequestBuffer(3)
	buf.AddRequest("p1", NetworkRequest{ID: "r1"})
	buf.AddRequest("p1", NetworkRequest{ID: "r2"})
	buf.AddRequest("p1", NetworkRequest{ID: "r3"})
	buf.AddRequest("p1", NetworkRequest{ID: "r4"})
	if buf.Count("p1") != 3 {
		t.Fatalf("expected 3 (max), got %d", buf.Count("p1"))
	}
	reqs := buf.GetRequests("p1")
	if reqs[0].ID == "r1" {
		t.Fatal("expected oldest (r1) to be dropped")
	}
}

func TestFrameRegistryRegisterAndGet(t *testing.T) {
	r := NewFrameRegistry()
	r.Register(FrameInfo{FrameID: "f1", URL: "https://iframe.example.com", Name: "login"})
	info, ok := r.Get("f1")
	if !ok {
		t.Fatal("expected frame f1")
	}
	if info.URL != "https://iframe.example.com" {
		t.Fatalf("expected URL, got %s", info.URL)
	}
}

func TestFrameRegistryUnregister(t *testing.T) {
	r := NewFrameRegistry()
	r.Register(FrameInfo{FrameID: "f1"})
	r.Unregister("f1")
	_, ok := r.Get("f1")
	if ok {
		t.Fatal("expected frame to be unregistered")
	}
}

func TestFrameSelectorResolveMainFrame(t *testing.T) {
	r := NewFrameRegistry()
	s := NewFrameSelector(r)
	info, err := s.ResolveFrame("")
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Fatal("expected nil for main frame")
	}
	if !s.IsMainFrame("") {
		t.Fatal("expected IsMainFrame true for empty selector")
	}
}

func TestFrameSelectorResolveByFrameID(t *testing.T) {
	r := NewFrameRegistry()
	r.Register(FrameInfo{FrameID: "f1", URL: "https://iframe.example.com"})
	s := NewFrameSelector(r)
	info, err := s.ResolveFrame("f1")
	if err != nil {
		t.Fatal(err)
	}
	if info == nil || info.FrameID != "f1" {
		t.Fatalf("expected frame f1, got %+v", info)
	}
}

func TestFrameSelectorResolveByName(t *testing.T) {
	r := NewFrameRegistry()
	r.Register(FrameInfo{FrameID: "f1", Name: "login-frame"})
	s := NewFrameSelector(r)
	info, err := s.ResolveFrame("login-frame")
	if err != nil {
		t.Fatal(err)
	}
	if info == nil || info.FrameID != "f1" {
		t.Fatalf("expected frame f1 by name, got %+v", info)
	}
}

func TestFrameSelectorResolveNotFound(t *testing.T) {
	r := NewFrameRegistry()
	s := NewFrameSelector(r)
	_, err := s.ResolveFrame("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent frame")
	}
}

func TestWaitForTextFound(t *testing.T) {
	provider := func() (string, error) {
		return "Hello World", nil
	}
	err := WaitForText(context.Background(), provider, "Hello", 1*time.Second)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWaitForTextTimeout(t *testing.T) {
	provider := func() (string, error) {
		return "Goodbye", nil
	}
	err := WaitForText(context.Background(), provider, "Hello", 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestWaitForTextDisappearFound(t *testing.T) {
	provider := func() (string, error) {
		return "No match here", nil
	}
	err := WaitForTextDisappear(context.Background(), provider, "Loading", 1*time.Second)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWaitForTextDisappearTimeout(t *testing.T) {
	provider := func() (string, error) {
		return "Still Loading...", nil
	}
	err := WaitForTextDisappear(context.Background(), provider, "Loading", 500*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestWaitForTextDefaultTimeout(t *testing.T) {
	provider := func() (string, error) {
		return "Found it", nil
	}
	err := WaitForText(context.Background(), provider, "Found", 0)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWaitForTextNilProvider(t *testing.T) {
	err := WaitForText(context.Background(), nil, "test", 1*time.Second)
	if err == nil {
		t.Fatal("expected error for nil provider")
	}
}
