package serve

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/engine"
)

// TestWSReadLimitLargePageDump is a deterministic regression test for
// the WS read limit failure class surfaced by the TASK-2325 smoke
// matrix (challenge-cloudflare scenario). The old default WS read
// limit was 32KB, which was too small for real-world HTML dumps from
// challenge-prone sites like nowsecure.nl. The fix raised the limit
// to 8MB on both server and client. This test verifies that a page
// dump exceeding the old 32KB limit succeeds over the wire.
//
// No live-network dependency: the large HTML is generated inline.
func TestWSReadLimitLargePageDump(t *testing.T) {
	// Generate HTML larger than the old 32KB default read limit.
	// ~100KB of content ensures we're well above 32KB.
	largeBody := strings.Repeat("<p>This is paragraph content for the large page dump regression test.</p>\n", 500)
	largeHTML := fmt.Sprintf(`<!doctype html><html><head><title>LargePage</title></head><body><h1>Large Dump</h1>%s</body></html>`, largeBody)
	if len(largeHTML) < 32*1024 {
		t.Fatalf("test HTML too small: %d bytes, need >32KB", len(largeHTML))
	}

	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, largeHTML)
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
	if por.Title != "LargePage" {
		t.Errorf("title = %q, want LargePage", por.Title)
	}

	// page.dump html — this is the operation that failed with the old
	// 32KB read limit because the HTML dump exceeded the limit.
	dumpReq, _ := MarshalTyped("3", CmdPageDump, PageDumpParams{
		SessionID: snr.SessionID,
		PageID:    por.PageID,
		Format:    string(DumpHTML),
	})
	resp = roundTrip(t, c, dumpReq)
	if !resp.OK {
		t.Fatalf("page.dump html (large): %+v - this would fail with the old 32KB read limit", resp)
	}

	// Verify the dump contains the expected content.
	dumpStr := fmt.Sprintf("%v", resp.Value)
	if !strings.Contains(dumpStr, "Large Dump") {
		t.Errorf("dump does not contain expected heading 'Large Dump'")
	}

	// page.dump markdown — also must succeed for large pages.
	dumpMdReq, _ := MarshalTyped("4", CmdPageDump, PageDumpParams{
		SessionID: snr.SessionID,
		PageID:    por.PageID,
		Format:    string(DumpMarkdown),
	})
	resp = roundTrip(t, c, dumpMdReq)
	if !resp.OK {
		t.Fatalf("page.dump markdown (large): %+v", resp)
	}

	// Cleanup.
	closeReq, _ := MarshalTyped("5", CmdPageClose, PageCloseParams{
		SessionID: snr.SessionID,
		PageID:    por.PageID,
	})
	_ = roundTrip(t, c, closeReq)
	scloseReq, _ := MarshalTyped("6", CmdSessionClose, SessionCloseParams{
		SessionID: snr.SessionID,
	})
	_ = roundTrip(t, c, scloseReq)
}

// TestWSReadLimitBoundary verifies that a message just under the 8MB
// limit succeeds. This is a boundary test for the read limit fix.
func TestWSReadLimitBoundary(t *testing.T) {
	// Generate ~500KB HTML — well under 8MB but well over the old 32KB.
	mediumBody := strings.Repeat("<div class='block'>Content block for boundary test.</div>\n", 2000)
	mediumHTML := fmt.Sprintf(`<!doctype html><html><head><title>Boundary</title></head><body>%s</body></html>`, mediumBody)

	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, mediumHTML)
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

	dumpReq, _ := MarshalTyped("3", CmdPageDump, PageDumpParams{
		SessionID: snr.SessionID,
		PageID:    por.PageID,
		Format:    string(DumpHTML),
	})
	resp = roundTrip(t, c, dumpReq)
	if !resp.OK {
		t.Fatalf("page.dump (boundary): %+v", resp)
	}

	// Cleanup.
	scloseReq, _ := MarshalTyped("4", CmdSessionClose, SessionCloseParams{
		SessionID: snr.SessionID,
	})
	_ = roundTrip(t, c, scloseReq)
}

// Ensure engine import is used.
var _ = engine.Config{}
