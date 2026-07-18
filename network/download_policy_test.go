package network

import (
	"errors"
	"testing"
)

func TestDownloadPolicyTypeAndSize(t *testing.T) {
	policy, err := NewPolicy(PolicyConfig{AllowedDownloadTypes: []string{"image/*", "application/pdf"}, MaxDownloadBytes: 8}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, contentType := range []string{"image/png", "image/jpeg", "application/pdf; charset=binary"} {
		if err := policy.ValidateDownload(contentType, 8, "session"); err != nil {
			t.Fatalf("ValidateDownload(%q): %v", contentType, err)
		}
	}
	for _, test := range []struct {
		contentType string
		size        int64
	}{
		{contentType: "text/plain", size: 1},
		{contentType: "image/png", size: 9},
		{contentType: "image/png", size: 0},
	} {
		if err := policy.ValidateDownload(test.contentType, test.size, "session"); !errors.Is(err, ErrPolicyDenied) {
			t.Fatalf("ValidateDownload(%q,%d)=%v", test.contentType, test.size, err)
		}
	}
}

func TestDownloadPolicyRejectsMalformedWildcard(t *testing.T) {
	for _, pattern := range []string{"*", "*/pdf", "application/", "application/p*df", "text /plain", "text/plain; charset=utf-8"} {
		if _, err := NewPolicy(PolicyConfig{AllowedDownloadTypes: []string{pattern}}, nil, nil); err == nil {
			t.Fatalf("pattern %q accepted", pattern)
		}
	}
}
