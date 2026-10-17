package tabs

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDialogStateIsBoundedImmutableAndClaimedByGeneration(t *testing.T) {
	handler := NewDialogHandler()
	if err := handler.Enqueue("tab", &PendingDialog{Type: DialogTypeAlert, URL: strings.Repeat("x", 8193)}); err == nil || handler.Pending("tab") != 0 {
		t.Fatal("oversized URL was truncated into a different policy identity")
	}
	input := &PendingDialog{Type: DialogTypePrompt, SessionID: "renderer", Message: strings.Repeat("ü", 5000), DefaultPrompt: "default", URL: "https://example.com"}
	if err := handler.Enqueue("tab", input); err != nil {
		t.Fatal(err)
	}
	input.DefaultPrompt = "mutated"
	pending, ok := handler.Peek("tab")
	if !ok || pending.Generation == 0 || pending.DefaultPrompt != "default" || len(pending.Message) > 8192 || !utf8.ValidString(pending.Message) {
		t.Fatalf("pending value=%+v", pending)
	}
	pending.DefaultPrompt = "external mutation"
	claimed, err := handler.Claim("tab", pending.Generation)
	if err != nil || claimed.DefaultPrompt != "default" {
		t.Fatalf("claim=%+v err=%v", claimed, err)
	}
	if _, err := handler.Claim("tab", pending.Generation); err == nil {
		t.Fatal("concurrent duplicate claim succeeded")
	}
	handler.Complete("tab", pending.Generation, false)
	if _, err := handler.Claim("tab", pending.Generation); err != nil {
		t.Fatalf("failed protocol reply could not retry: %v", err)
	}
	handler.ClearSession("tab", "renderer")
	if err := handler.Enqueue("tab", &PendingDialog{Type: DialogTypeAlert, SessionID: "renderer", Message: "new dialog"}); err != nil {
		t.Fatal(err)
	}
	handler.Complete("tab", pending.Generation, true)
	newDialog, ok := handler.Peek("tab")
	if !ok || newDialog.Message != "new dialog" || newDialog.Generation == pending.Generation {
		t.Fatal("stale completion removed newer dialog")
	}
	for range 31 {
		if err := handler.Enqueue("tab", NewAlertDialog("queued", "https://example.com")); err != nil {
			t.Fatal(err)
		}
	}
	if err := handler.Enqueue("tab", NewAlertDialog("overflow", "https://example.com")); err == nil || handler.Pending("tab") != 32 {
		t.Fatalf("dialog budget not enforced: count=%d err=%v", handler.Pending("tab"), err)
	}
}
