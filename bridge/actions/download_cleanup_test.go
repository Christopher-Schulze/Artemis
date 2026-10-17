package actions

import (
	"errors"
	"testing"
)

func TestDownloadCleanupFailuresKeepPrimaryCauseAndRevokeSuccess(t *testing.T) {
	success := Outcome{Success: true, Download: &Download{Size: 3}}
	if got := withCleanupFailure(success, "download deny reset", nil); !got.Success || got.Error != "" {
		t.Fatalf("confirmed cleanup changed a verified download: %+v", got)
	}
	unreset := withCleanupFailure(success, "download deny reset", errors.New("setDownloadBehavior refused"))
	if unreset.Success || unreset.Failure != FailureProtocol || unreset.Error != "download deny reset: setDownloadBehavior refused" {
		t.Fatalf("unconfirmed deny reset still reported success: %+v", unreset)
	}
	policy := Outcome{Failure: FailurePolicy, Error: "download exceeds quota"}
	uncancelled := withCleanupFailure(policy, "download cancel", errors.New("cancelDownload timed out"))
	if uncancelled.Success || uncancelled.Failure != FailurePolicy || uncancelled.Error != "download exceeds quota; download cancel: cancelDownload timed out" {
		t.Fatalf("cleanup failure replaced the primary cause: %+v", uncancelled)
	}
}
