package telemetry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTraceDiscardArchiveCompletesWithoutCreatingOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "must-not-be-created")
	recorder := NewTraceRecorder(TraceRecordConfig{TraceDir: path, DiscardArchive: true, Snapshots: true})
	for range 2 {
		if err := recorder.Start(); err != nil {
			t.Fatal(err)
		}
		if err := recorder.AddSnapshot([]byte("actual captured snapshot")); err != nil {
			t.Fatal(err)
		}
		if output, err := recorder.Stop(); err != nil || output != "" || recorder.IsActive() || recorder.retainedBytes != 0 || len(recorder.snapshots) != 0 {
			t.Fatalf("discard did not finish and release its owner: %q %v", output, err)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("discarded trace created persistent output: %v", err)
	}
}
