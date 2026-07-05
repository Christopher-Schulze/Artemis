package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Christopher-Schulze/Artemis/engine"
)

// TestProtocolVersionHandshake verifies that the version meta-command
// returns the current ProtocolVersion.
func TestProtocolVersionHandshake(t *testing.T) {
	addr, cleanup := startServer(t)
	defer cleanup()
	c := dial(t, addr)
	defer c.CloseNow()

	resp := roundTrip(t, c, Request{ID: "v", Cmd: "version"})
	if !resp.OK {
		t.Fatalf("version: %+v", resp)
	}
	var vr VersionResponse
	if err := DecodeTypedResult(&resp, &vr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if vr.Protocol != ProtocolVersion {
		t.Errorf("protocol = %q, want %q", vr.Protocol, ProtocolVersion)
	}
	if vr.Server != "artemis-serve" {
		t.Errorf("server = %q, want artemis-serve", vr.Server)
	}
}

// TestMarshalTypedAndDecodeParams verifies the typed helpers round-trip
// correctly through the wire format.
func TestMarshalTypedAndDecodeParams(t *testing.T) {
	want := true
	req, err := MarshalTyped("a1", CmdPageAssert, PageAssertParams{
		SessionID: "s1",
		PageID:    "p1",
		Mode:      string(AssertSelectorExists),
		Selector:  "#hi",
		Want:      &want,
	})
	if err != nil {
		t.Fatalf("MarshalTyped: %v", err)
	}
	if req.ID != "a1" || req.Cmd != string(CmdPageAssert) {
		t.Errorf("req = %+v", req)
	}
	var p PageAssertParams
	if err := DecodeParams(&req, &p); err != nil {
		t.Fatalf("DecodeParams: %v", err)
	}
	if p.SessionID != "s1" || p.PageID != "p1" || p.Selector != "#hi" {
		t.Errorf("params = %+v", p)
	}
	if p.Mode != string(AssertSelectorExists) {
		t.Errorf("mode = %q", p.Mode)
	}
	if p.Want == nil || *p.Want != true {
		t.Errorf("want = %v", p.Want)
	}
}

// TestDecodeTypedResultError verifies that DecodeTypedResult returns
// an error containing the error code for non-OK responses.
func TestDecodeTypedResultError(t *testing.T) {
	resp := &Response{
		ID:    "x",
		OK:    false,
		Error: &Err{Code: string(ErrNoSession), Message: "unknown sessionId"},
	}
	var result EmptyResult
	err := DecodeTypedResult(resp, &result)
	if err == nil {
		t.Fatal("expected error for non-OK response")
	}
	if code := string(ErrNoSession); !contains(err.Error(), code) {
		t.Errorf("error %q does not contain code %q", err.Error(), code)
	}
}

// TestDecodeTypedResultOK verifies that DecodeTypedResult populates
// the target struct for OK responses.
func TestDecodeTypedResultOK(t *testing.T) {
	resp := &Response{
		ID:    "x",
		OK:    true,
		Value: map[string]any{"sessionId": "s42"},
	}
	var result SessionNewResult
	if err := DecodeTypedResult(resp, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.SessionID != "s42" {
		t.Errorf("sessionId = %q, want s42", result.SessionID)
	}
}

// TestConformanceFullOpSet drives every published command over the
// wire against a live in-process server + httptest page and asserts
// structured responses. This is the same-TASK conformance test
// required by TASK-2326.
func TestConformanceFullOpSet(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><head><title>Conformance</title></head><body><h1 id="hi">Hello</h1><input id="q" type="text" value=""></body></html>`)
	}))
	defer page.Close()

	addr, cleanup := startServer(t)
	defer cleanup()
	c := dial(t, addr)
	defer c.CloseNow()

	// 1. version handshake
	resp := roundTrip(t, c, Request{ID: "1", Cmd: "version"})
	if !resp.OK {
		t.Fatalf("version: %+v", resp)
	}

	// 2. session.new
	resp = roundTrip(t, c, Request{ID: "2", Cmd: "session.new"})
	if !resp.OK {
		t.Fatalf("session.new: %+v", resp)
	}
	var snr SessionNewResult
	mustDecode(t, resp, &snr)

	// 3. page.open
	openReq, _ := MarshalTyped("3", CmdPageOpen, PageOpenParams{
		SessionID:  snr.SessionID,
		URL:        page.URL,
		RunScripts: false,
	})
	resp = roundTrip(t, c, openReq)
	if !resp.OK {
		t.Fatalf("page.open: %+v", resp)
	}
	var por PageOpenResult
	mustDecode(t, resp, &por)
	if por.Title != "Conformance" {
		t.Errorf("title = %q, want Conformance", por.Title)
	}
	if por.Status != 200 {
		t.Errorf("status = %d, want 200", por.Status)
	}

	// 4. page.eval
	evalReq, _ := MarshalTyped("4", CmdPageEval, PageEvalParams{
		SessionID: snr.SessionID,
		PageID:    por.PageID,
		Expr:      "document.title",
	})
	resp = roundTrip(t, c, evalReq)
	if !resp.OK {
		t.Fatalf("page.eval: %+v", resp)
	}
	var er PageEvalResult
	mustDecode(t, resp, &er)
	if er.Value != "Conformance" {
		t.Errorf("eval value = %q, want Conformance", er.Value)
	}

	// 5. page.dump markdown
	dumpReq, _ := MarshalTyped("5", CmdPageDump, PageDumpParams{
		SessionID: snr.SessionID,
		PageID:    por.PageID,
		Format:    string(DumpMarkdown),
	})
	resp = roundTrip(t, c, dumpReq)
	if !resp.OK {
		t.Fatalf("page.dump: %+v", resp)
	}

	// 6. page.type
	typeReq, _ := MarshalTyped("6", CmdPageType, PageTypeParams{
		SessionID: snr.SessionID,
		PageID:    por.PageID,
		Selector:  "#q",
		Text:      "typed",
	})
	resp = roundTrip(t, c, typeReq)
	if !resp.OK {
		t.Fatalf("page.type: %+v", resp)
	}

	// 7. page.assert selector_exists
	wantTrue := true
	assertReq, _ := MarshalTyped("7", CmdPageAssert, PageAssertParams{
		SessionID: snr.SessionID,
		PageID:    por.PageID,
		Mode:      string(AssertSelectorExists),
		Selector:  "#hi",
		Want:      &wantTrue,
	})
	resp = roundTrip(t, c, assertReq)
	if !resp.OK {
		t.Fatalf("page.assert: %+v", resp)
	}
	var ar AssertResult
	mustDecode(t, resp, &ar)
	if !ar.Pass {
		t.Errorf("assert pass = %v, want true", ar.Pass)
	}

	// 8. page.wait_idle
	waitReq, _ := MarshalTyped("8", CmdPageWaitIdle, PageWaitIdleParams{
		SessionID: snr.SessionID,
		PageID:    por.PageID,
	})
	resp = roundTrip(t, c, waitReq)
	if !resp.OK {
		t.Fatalf("page.wait_idle: %+v", resp)
	}

	// 9. page.close
	closeReq, _ := MarshalTyped("9", CmdPageClose, PageCloseParams{
		SessionID: snr.SessionID,
		PageID:    por.PageID,
	})
	resp = roundTrip(t, c, closeReq)
	if !resp.OK {
		t.Fatalf("page.close: %+v", resp)
	}

	// 10. session.close
	scloseReq, _ := MarshalTyped("10", CmdSessionClose, SessionCloseParams{
		SessionID: snr.SessionID,
	})
	resp = roundTrip(t, c, scloseReq)
	if !resp.OK {
		t.Fatalf("session.close: %+v", resp)
	}
}

// TestConformanceErrorTaxonomy verifies that each error code is
// returned for its corresponding failure condition.
func TestConformanceErrorTaxonomy(t *testing.T) {
	addr, cleanup := startServer(t)
	defer cleanup()
	c := dial(t, addr)
	defer c.CloseNow()

	// unknown_cmd
	resp := roundTrip(t, c, Request{ID: "1", Cmd: "bogus"})
	if resp.OK || resp.Error == nil || resp.Error.Code != string(ErrUnknownCmd) {
		t.Errorf("unknown_cmd: got %+v", resp)
	}

	// no_session (page.open without session.new)
	openReq, _ := MarshalTyped("2", CmdPageOpen, PageOpenParams{
		SessionID: "nonexistent",
		URL:       "http://127.0.0.1:1/nope",
	})
	resp = roundTrip(t, c, openReq)
	if resp.OK || resp.Error == nil || resp.Error.Code != string(ErrNoSession) {
		t.Errorf("no_session: got %+v", resp)
	}

	// bad_params (params is valid JSON but wrong type for the command)
	resp = roundTrip(t, c, Request{
		ID:     "3",
		Cmd:    "session.close",
		Params: json.RawMessage(`"not-an-object"`),
	})
	if resp.OK || resp.Error == nil || resp.Error.Code != string(ErrBadParams) {
		t.Errorf("bad_params: got OK=%v error=%+v", resp.OK, resp.Error)
	}
}

func mustDecode(t *testing.T, resp Response, target interface{}) {
	t.Helper()
	if err := DecodeTypedResult(&resp, target); err != nil {
		t.Fatalf("decode result: %v", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && stringContains(s, sub)))
}

// stringContains is a local strings.Contains to avoid importing
// strings in this test file (the non-test file already imports it).
func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Ensure engine is imported so the test file compiles even if some
// imports are trimmed by formatters.
var _ = engine.Config{}
