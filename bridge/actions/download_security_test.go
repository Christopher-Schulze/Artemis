package actions

import (
	"strings"
	"testing"
)

func TestDownloadRequestRejectsCallerControlledDirectory(t *testing.T) {
	err := validateRequest(Request{Kind: KindDownload, Ref: "e1", DownloadDir: "/tmp/unowned"})
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("error=%v", err)
	}
	if err := validateRequest(Request{Kind: KindDownload, Ref: "e1"}); err != nil {
		t.Fatalf("owned download request rejected: %v", err)
	}
}
