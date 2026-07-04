package serve

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestTextDriftTolerantAssertion is a deterministic regression test
// for the text-drift failure class surfaced by the TASK-2325 smoke
// matrix (nav-example scenario). example.com changed its body text
// from "illustrative examples" to "documentation examples", breaking
// an exact-match assertion. The fix was to use text_contains with a
// stable substring. This test verifies that text_contains assertions
// are drift-tolerant: they match on partial substrings, not exact
// full-text equality.
//
// No live-network dependency: the HTML fixtures simulate the old and
// new example.com content inline.
func TestTextDriftTolerantAssertion(t *testing.T) {
	// Simulate the old example.com text (pre-drift).
	oldHTML := `<!doctype html><html><head><title>Example Domain</title></head><body><div><h1>Example Domain</h1><p>This domain is for use in illustrative examples in documents.</p></div></body></html>`
	// Simulate the new example.com text (post-drift).
	newHTML := `<!doctype html><html><head><title>Example Domain</title></head><body><div><h1>Example Domain</h1><p>This domain is for use in documentation examples. You may use this domain in literature without prior coordination.</p></div></body></html>`

	// Each case specifies a substring, whether we expect it to be
	// found (wantFound), and whether the assertion should pass
	// (wantPass = true when wantFound matches reality).
	// ar.Pass means "assertion passed" = (found == wantFound).
	tests := []struct {
		name      string
		html      string
		substr    string
		wantFound bool // what we set as Want in the assert params
		wantPass  bool // whether the assertion should pass (wantFound == actual)
	}{
		// The stable substring "examples" appears in both old and new
		// HTML, making it drift-tolerant.
		{"old_html_stable_substr", oldHTML, "examples", true, true},
		{"new_html_stable_substr", newHTML, "examples", true, true},
		// The old exact-match "illustrative examples" only matches old HTML.
		{"old_html_drifted_substr", oldHTML, "illustrative examples", true, true},
		// The new exact-match "documentation examples" only matches new HTML.
		{"new_html_correct_substr", newHTML, "documentation examples", true, true},
		// A substring that appears in neither: assert it's NOT present (want=false).
		// The assertion passes because found=false == want=false.
		{"neither_html_nonexistent", newHTML, "nonexistent text here", false, true},
		// Title is stable across both versions.
		{"old_html_title", oldHTML, "Example Domain", true, true},
		{"new_html_title", newHTML, "Example Domain", true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, tc.html)
			}))
			defer page.Close()

			addr, cleanup := startServer(t)
			defer cleanup()
			c := dial(t, addr)
			defer c.CloseNow()

			// session.new
			resp := roundTrip(t, c, Request{ID: "1", Cmd: "session.new"})
			if !resp.OK {
				t.Fatalf("session.new: %+v", resp)
			}
			var snr SessionNewResult
			mustDecode(t, resp, &snr)

			// page.open
			openReq, _ := MarshalTyped("2", CmdPageOpen, PageOpenParams{
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

			// page.assert text_contains
			wantFound := tc.wantFound
			assertReq, _ := MarshalTyped("3", CmdPageAssert, PageAssertParams{
				SessionID: snr.SessionID,
				PageID:    por.PageID,
				Mode:      string(AssertTextContains),
				Substring: tc.substr,
				Want:      &wantFound,
			})
			resp = roundTrip(t, c, assertReq)
			if !resp.OK {
				t.Fatalf("page.assert: %+v", resp)
			}
			var ar AssertResult
			mustDecode(t, resp, &ar)
			if ar.Pass != tc.wantPass {
				t.Errorf("text_contains %q (wantFound=%v): pass=%v, want %v", tc.substr, tc.wantFound, ar.Pass, tc.wantPass)
			}

			// Cleanup.
			scloseReq, _ := MarshalTyped("4", CmdSessionClose, SessionCloseParams{
				SessionID: snr.SessionID,
			})
			_ = roundTrip(t, c, scloseReq)
		})
	}
}

// TestTitleContainsDriftTolerant verifies that title_contains
// assertions match on partial substrings of the title.
func TestTitleContainsDriftTolerant(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><head><title>Example Domain - Free for Use</title></head><body></body></html>`)
	}))
	defer page.Close()

	addr, cleanup := startServer(t)
	defer cleanup()
	c := dial(t, addr)
	defer c.CloseNow()

	resp := roundTrip(t, c, Request{ID: "1", Cmd: "session.new"})
	if !resp.OK {
		t.Fatalf("session.new: %+v", resp)
	}
	var snr SessionNewResult
	mustDecode(t, resp, &snr)

	openReq, _ := MarshalTyped("2", CmdPageOpen, PageOpenParams{
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

	// Partial title match must pass.
	want := true
	assertReq, _ := MarshalTyped("3", CmdPageAssert, PageAssertParams{
		SessionID: snr.SessionID,
		PageID:    por.PageID,
		Mode:      string(AssertTitleContains),
		Substring: "Example",
		Want:      &want,
	})
	resp = roundTrip(t, c, assertReq)
	if !resp.OK {
		t.Fatalf("page.assert: %+v", resp)
	}
	var ar AssertResult
	mustDecode(t, resp, &ar)
	if !ar.Pass {
		t.Errorf("title_contains 'Example': pass=false, want true")
	}

	// Non-matching title must fail (want=true but found=false → pass=false).
	want = true
	assertReq2, _ := MarshalTyped("4", CmdPageAssert, PageAssertParams{
		SessionID: snr.SessionID,
		PageID:    por.PageID,
		Mode:      string(AssertTitleContains),
		Substring: "Nonexistent",
		Want:      &want,
	})
	resp = roundTrip(t, c, assertReq2)
	if !resp.OK {
		t.Fatalf("page.assert: %+v", resp)
	}
	mustDecode(t, resp, &ar)
	if ar.Pass {
		t.Errorf("title_contains 'Nonexistent': pass=true, want false")
	}

	// Cleanup.
	scloseReq, _ := MarshalTyped("5", CmdSessionClose, SessionCloseParams{
		SessionID: snr.SessionID,
	})
	_ = roundTrip(t, c, scloseReq)
}
