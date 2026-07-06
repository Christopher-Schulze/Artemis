package bridge

import (
	"context"
	"testing"
	"time"
)

func TestFormActionsSetChecked(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	fa.UpdateRefs([]RefHandle{{Ref: "e5", Handle: 101, Role: "checkbox"}})
	err := fa.SetChecked(context.Background(), "e5", true)
	if err != nil {
		t.Fatal(err)
	}
	cmds := fa.Commands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	if cmds[0].Type != FormCommandSetChecked {
		t.Errorf("expected set_checked, got %s", cmds[0].Type)
	}
	if cmds[0].Ref != "e5" {
		t.Errorf("expected ref e5, got %s", cmds[0].Ref)
	}
	if !cmds[0].Checked {
		t.Error("expected checked=true")
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

func TestFormActionsSetCheckedRefNotInSnapshot(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	err := fa.SetChecked(context.Background(), "e99", true)
	if err == nil {
		t.Fatal("expected error for ref not in snapshot")
	}
}

func TestFormActionsSelectOption(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	fa.UpdateRefs([]RefHandle{{Ref: "e3", Handle: 103, Role: "combobox"}})
	err := fa.SelectOption(context.Background(), "e3", "Option 1")
	if err != nil {
		t.Fatal(err)
	}
	cmds := fa.Commands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	if cmds[0].Type != FormCommandSelectOption {
		t.Errorf("expected select_option, got %s", cmds[0].Type)
	}
	if cmds[0].Value != "Option 1" {
		t.Errorf("expected value 'Option 1', got %s", cmds[0].Value)
	}
}

func TestFormActionsSelectOptionNoValue(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	fa.UpdateRefs([]RefHandle{{Ref: "e3", Handle: 103, Role: "combobox"}})
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

// --- FormActions: FillInput / FillSlider / ResolveRef (L4323) ---

func TestFormActionsFillInput(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	fa.UpdateRefs([]RefHandle{{Ref: "e7", Handle: 107, Role: "textbox"}})
	err := fa.FillInput(context.Background(), "e7", "hello world")
	if err != nil {
		t.Fatal(err)
	}
	cmds := fa.Commands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	if cmds[0].Type != FormCommandFillInput {
		t.Errorf("expected fill_input, got %s", cmds[0].Type)
	}
	if cmds[0].Value != "hello world" {
		t.Errorf("expected value 'hello world', got %s", cmds[0].Value)
	}
}

func TestFormActionsFillInputNoRef(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	err := fa.FillInput(context.Background(), "", "hello")
	if err == nil {
		t.Fatal("expected error for empty ref")
	}
}

func TestFormActionsFillInputNoSession(t *testing.T) {
	fa := NewFormActions(nil)
	err := fa.FillInput(context.Background(), "e7", "hello")
	if err == nil {
		t.Fatal("expected error for nil session")
	}
}

func TestFormActionsFillInputRefNotInSnapshot(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	err := fa.FillInput(context.Background(), "e99", "hello")
	if err == nil {
		t.Fatal("expected error for ref not in snapshot")
	}
}

func TestFormActionsFillSlider(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	fa.UpdateRefs([]RefHandle{{Ref: "e9", Handle: 109, Role: "slider"}})
	err := fa.FillSlider(context.Background(), "e9", 50.0)
	if err != nil {
		t.Fatal(err)
	}
	cmds := fa.Commands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	if cmds[0].Type != FormCommandFillSlider {
		t.Errorf("expected fill_slider, got %s", cmds[0].Type)
	}
	if cmds[0].Slider != 50.0 {
		t.Errorf("expected slider 50.0, got %f", cmds[0].Slider)
	}
}

func TestFormActionsFillSliderNoRef(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	err := fa.FillSlider(context.Background(), "", 50.0)
	if err == nil {
		t.Fatal("expected error for empty ref")
	}
}

func TestFormActionsFillSliderRefNotInSnapshot(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	err := fa.FillSlider(context.Background(), "e99", 50.0)
	if err == nil {
		t.Fatal("expected error for ref not in snapshot")
	}
}

func TestFormActionsFillSliderFromString(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	fa.UpdateRefs([]RefHandle{{Ref: "e9", Handle: 109, Role: "slider"}})
	err := fa.FillSliderFromString(context.Background(), "e9", "75.5")
	if err != nil {
		t.Fatal(err)
	}
	cmds := fa.Commands()
	if len(cmds) != 1 || cmds[0].Slider != 75.5 {
		t.Fatalf("expected slider 75.5, got %+v", cmds)
	}
}

func TestFormActionsFillSliderFromStringInvalid(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	fa.UpdateRefs([]RefHandle{{Ref: "e9", Handle: 109, Role: "slider"}})
	err := fa.FillSliderFromString(context.Background(), "e9", "not-a-number")
	if err == nil {
		t.Fatal("expected error for invalid slider value")
	}
}

func TestFormActionsResolveRef(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	fa.UpdateRefs([]RefHandle{{Ref: "e5", Handle: 105, Role: "button", Name: "Submit"}})
	handle, err := fa.ResolveRef(context.Background(), "e5")
	if err != nil {
		t.Fatal(err)
	}
	if handle.Handle != 105 {
		t.Errorf("expected handle 105, got %d", handle.Handle)
	}
	if handle.Role != "button" {
		t.Errorf("expected role button, got %s", handle.Role)
	}
	if handle.Name != "Submit" {
		t.Errorf("expected name Submit, got %s", handle.Name)
	}
}

func TestFormActionsResolveRefNotFound(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	_, err := fa.ResolveRef(context.Background(), "e99")
	if err == nil {
		t.Fatal("expected error for ref not in snapshot")
	}
}

func TestFormActionsResolveRefNoRef(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	_, err := fa.ResolveRef(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty ref")
	}
}

func TestFormActionsResolveRefNoSession(t *testing.T) {
	fa := NewFormActions(nil)
	_, err := fa.ResolveRef(context.Background(), "e5")
	if err == nil {
		t.Fatal("expected error for nil session")
	}
}

func TestRefRegistryRegisterAndGet(t *testing.T) {
	r := NewRefRegistry()
	r.Register(RefHandle{Ref: "e1", Handle: 11, Role: "textbox"})
	r.Register(RefHandle{Ref: "e2", Handle: 22, Role: "checkbox"})
	if r.Count() != 2 {
		t.Fatalf("expected 2 refs, got %d", r.Count())
	}
	h, ok := r.Get("e1")
	if !ok {
		t.Fatal("expected ref e1")
	}
	if h.Handle != 11 {
		t.Errorf("expected handle 11, got %d", h.Handle)
	}
}

func TestRefRegistryHas(t *testing.T) {
	r := NewRefRegistry()
	r.Register(RefHandle{Ref: "e1", Handle: 11})
	if !r.Has("e1") {
		t.Error("expected Has(e1) true")
	}
	if r.Has("e99") {
		t.Error("expected Has(e99) false")
	}
}

func TestRefRegistryClear(t *testing.T) {
	r := NewRefRegistry()
	r.Register(RefHandle{Ref: "e1", Handle: 11})
	r.Clear()
	if r.Count() != 0 {
		t.Errorf("expected 0 after clear, got %d", r.Count())
	}
}

func TestRefRegistryFrame(t *testing.T) {
	r := NewRefRegistry()
	r.SetFrame("frame-abc")
	if r.Frame() != "frame-abc" {
		t.Errorf("expected frame-abc, got %s", r.Frame())
	}
}

func TestRefRegistryAll(t *testing.T) {
	r := NewRefRegistry()
	r.Register(RefHandle{Ref: "e1", Handle: 11})
	r.Register(RefHandle{Ref: "e2", Handle: 22})
	all := r.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(all))
	}
}

func TestFormActionsMultipleCommands(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	fa := NewFormActions(session)
	fa.UpdateRefs([]RefHandle{
		{Ref: "e1", Handle: 11, Role: "textbox"},
		{Ref: "e2", Handle: 22, Role: "checkbox"},
		{Ref: "e3", Handle: 33, Role: "slider"},
	})
	fa.FillInput(context.Background(), "e1", "hello")
	fa.SetChecked(context.Background(), "e2", true)
	fa.FillSlider(context.Background(), "e3", 75.0)
	cmds := fa.Commands()
	if len(cmds) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(cmds))
	}
	if cmds[0].Type != FormCommandFillInput {
		t.Errorf("expected fill_input first, got %s", cmds[0].Type)
	}
	if cmds[1].Type != FormCommandSetChecked {
		t.Errorf("expected set_checked second, got %s", cmds[1].Type)
	}
	if cmds[2].Type != FormCommandFillSlider {
		t.Errorf("expected fill_slider third, got %s", cmds[2].Type)
	}
}

func TestFormActionsNewWithRefs(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	refs := NewRefRegistry()
	refs.Register(RefHandle{Ref: "e1", Handle: 11, Role: "textbox"})
	fa := NewFormActionsWithRefs(session, refs)
	if !fa.Refs().Has("e1") {
		t.Fatal("expected ref e1 from pre-populated registry")
	}
	err := fa.FillInput(context.Background(), "e1", "test")
	if err != nil {
		t.Fatal(err)
	}
}

// --- NetworkRequestBuffer: URL-based matching (L4324) ---

func TestNetworkRequestBufferFindByURLSubstring(t *testing.T) {
	buf := NewNetworkRequestBuffer(100)
	buf.AddRequest("p1", NetworkRequest{ID: "r1", URL: "https://api.example.com/users"})
	buf.AddRequest("p1", NetworkRequest{ID: "r2", URL: "https://api.example.com/posts"})
	buf.AddRequest("p1", NetworkRequest{ID: "r3", URL: "https://cdn.example.com/script.js"})

	matches := buf.FindByURL("p1", "api.example.com")
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
}

func TestNetworkRequestBufferFindByURLWildcard(t *testing.T) {
	buf := NewNetworkRequestBuffer(100)
	buf.AddRequest("p1", NetworkRequest{ID: "r1", URL: "https://api.example.com/v1/users"})
	buf.AddRequest("p1", NetworkRequest{ID: "r2", URL: "https://api.example.com/v2/posts"})
	buf.AddRequest("p1", NetworkRequest{ID: "r3", URL: "https://cdn.example.com/script.js"})

	matches := buf.FindByURL("p1", "*api.example.com*/v*")
	if len(matches) != 2 {
		t.Fatalf("expected 2 wildcard matches, got %d", len(matches))
	}
}

func TestNetworkRequestBufferFindByURLMatchAll(t *testing.T) {
	buf := NewNetworkRequestBuffer(100)
	buf.AddRequest("p1", NetworkRequest{ID: "r1", URL: "https://a.com"})
	buf.AddRequest("p1", NetworkRequest{ID: "r2", URL: "https://b.com"})

	matches := buf.FindByURL("p1", "*")
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches for * pattern, got %d", len(matches))
	}
}

func TestNetworkRequestBufferFindByURLEmptyPattern(t *testing.T) {
	buf := NewNetworkRequestBuffer(100)
	buf.AddRequest("p1", NetworkRequest{ID: "r1", URL: "https://a.com"})
	matches := buf.FindByURL("p1", "")
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches for empty pattern, got %d", len(matches))
	}
}

func TestNetworkRequestBufferFindByURLNoMatch(t *testing.T) {
	buf := NewNetworkRequestBuffer(100)
	buf.AddRequest("p1", NetworkRequest{ID: "r1", URL: "https://a.com"})
	matches := buf.FindByURL("p1", "nomatch")
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matches))
	}
}

func TestNetworkRequestBufferFindByMethod(t *testing.T) {
	buf := NewNetworkRequestBuffer(100)
	buf.AddRequest("p1", NetworkRequest{ID: "r1", Method: "GET"})
	buf.AddRequest("p1", NetworkRequest{ID: "r2", Method: "POST"})
	buf.AddRequest("p1", NetworkRequest{ID: "r3", Method: "get"})

	matches := buf.FindByMethod("p1", "GET")
	if len(matches) != 2 {
		t.Fatalf("expected 2 GET matches (case-insensitive), got %d", len(matches))
	}
}

func TestNetworkRequestBufferFindByStatusRange(t *testing.T) {
	buf := NewNetworkRequestBuffer(100)
	buf.AddRequest("p1", NetworkRequest{ID: "r1", Status: 200})
	buf.AddRequest("p1", NetworkRequest{ID: "r2", Status: 404})
	buf.AddRequest("p1", NetworkRequest{ID: "r3", Status: 500})

	matches := buf.FindByStatusRange("p1", 400, 599)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches in 400-599, got %d", len(matches))
	}
}

func TestNetworkRequestBufferFindByResourceType(t *testing.T) {
	buf := NewNetworkRequestBuffer(100)
	buf.AddRequest("p1", NetworkRequest{ID: "r1", ResourceType: "Document"})
	buf.AddRequest("p1", NetworkRequest{ID: "r2", ResourceType: "Script"})
	buf.AddRequest("p1", NetworkRequest{ID: "r3", ResourceType: "Document"})

	matches := buf.FindByResourceType("p1", "Document")
	if len(matches) != 2 {
		t.Fatalf("expected 2 Document matches, got %d", len(matches))
	}
}

func TestNetworkRequestBufferFindFailed(t *testing.T) {
	buf := NewNetworkRequestBuffer(100)
	buf.AddRequest("p1", NetworkRequest{ID: "r1", Status: 200, OK: true})
	buf.AddRequest("p1", NetworkRequest{ID: "r2", Status: 404, OK: false})
	buf.AddRequest("p1", NetworkRequest{ID: "r3", Status: 500, OK: false, FailureText: "Internal Server Error"})
	buf.AddRequest("p1", NetworkRequest{ID: "r4", Status: 0, OK: false, FailureText: "net::ERR_CONNECTION_REFUSED"})

	matches := buf.FindFailed("p1")
	if len(matches) != 3 {
		t.Fatalf("expected 3 failed matches, got %d", len(matches))
	}
}

func TestURLMatchesPattern(t *testing.T) {
	tests := []struct {
		url     string
		pattern string
		want    bool
	}{
		{"https://example.com", "*", true},
		{"https://example.com", "", false},
		{"https://api.example.com/v1/users", "api.example.com", true},
		{"https://cdn.example.com/script.js", "api.example.com", false},
		{"https://api.example.com/v1/users", "*api*/v*", true},
		{"https://api.example.com/v2/posts", "*api*/v*", true},
		{"https://cdn.example.com/script.js", "*api*/v*", false},
		{"https://example.com/path", "*/path", true},
		{"https://example.com/other", "*/path", false},
		{"https://example.com", "example.com/path", false},
	}
	for _, tt := range tests {
		got := urlMatchesPattern(tt.url, tt.pattern)
		if got != tt.want {
			t.Errorf("urlMatchesPattern(%q, %q) = %v, want %v", tt.url, tt.pattern, got, tt.want)
		}
	}
}
