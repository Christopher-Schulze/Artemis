package bridge

import (
	"context"
	"testing"
)

func TestCheckHealthRecoveryHealthy(t *testing.T) {
	h := NewHealthChecker(2)
	st, err := CheckHealthRecovery(func() bool { return true }, h, 2)
	if err != nil || st != HealthHealthy {
		t.Fatalf("st=%s err=%v", st, err)
	}
}

func TestNavigateSnapshotBatchesCommands(t *testing.T) {
	var flushed int
	b := NewBatcher(func(ctx context.Context, cmds []Command) ([]Response, error) {
		flushed = len(cmds)
		return nil, nil
	}, 4)
	if err := NavigateSnapshot(context.Background(), b, "https://example.com"); err != nil {
		t.Fatal(err)
	}
	if flushed != 2 {
		t.Fatalf("flushed=%d", flushed)
	}
}
