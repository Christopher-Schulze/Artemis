package observe

import (
	"sync"
	"testing"
	"time"
)

func TestConsoleLevel_IsValid(t *testing.T) {
	tests := []struct {
		level ConsoleLevel
		want  bool
	}{
		{ConsoleLevelLog, true},
		{ConsoleLevelWarn, true},
		{ConsoleLevelError, true},
		{ConsoleLevelInfo, true},
		{ConsoleLevelDebug, true},
		{ConsoleLevel("invalid"), false},
		{ConsoleLevel(""), false},
	}
	for _, tt := range tests {
		if got := tt.level.IsValid(); got != tt.want {
			t.Errorf("%s.IsValid() = %v, want %v", tt.level, got, tt.want)
		}
	}
}

func TestNormalizeLevel(t *testing.T) {
	tests := []struct {
		input string
		want  ConsoleLevel
	}{
		{"log", ConsoleLevelLog},
		{"warning", ConsoleLevelWarn},
		{"error", ConsoleLevelError},
		{"info", ConsoleLevelInfo},
		{"debug", ConsoleLevelDebug},
		{"unknown", ConsoleLevelLog},
		{"", ConsoleLevelLog},
	}
	for _, tt := range tests {
		if got := NormalizeLevel(tt.input); got != tt.want {
			t.Errorf("NormalizeLevel(%q) = %s, want %s", tt.input, got, tt.want)
		}
	}
}

func TestNewConsoleRingBuffer_DefaultCapacity(t *testing.T) {
	r := NewConsoleRingBuffer(0)
	if r.Cap() != DefaultConsoleBufferSize {
		t.Fatalf("expected default cap=%d, got %d", DefaultConsoleBufferSize, r.Cap())
	}
}

func TestNewConsoleRingBuffer_NegativeCapacity(t *testing.T) {
	r := NewConsoleRingBuffer(-1)
	if r.Cap() != DefaultConsoleBufferSize {
		t.Fatalf("expected default cap=%d, got %d", DefaultConsoleBufferSize, r.Cap())
	}
}

func TestNewConsoleRingBuffer_CustomCapacity(t *testing.T) {
	r := NewConsoleRingBuffer(100)
	if r.Cap() != 100 {
		t.Fatalf("expected cap=100, got %d", r.Cap())
	}
}

func TestConsoleRingBuffer_PushAndSnapshot(t *testing.T) {
	r := NewConsoleRingBuffer(10)
	r.PushMessage(ConsoleLevelLog, []string{"hello"})
	r.PushMessage(ConsoleLevelError, []string{"error"})

	entries := r.Snapshot()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Level != ConsoleLevelLog {
		t.Errorf("expected first=log, got %s", entries[0].Level)
	}
	if entries[0].Args[0] != "hello" {
		t.Errorf("expected first arg=hello, got %s", entries[0].Args[0])
	}
	if entries[1].Level != ConsoleLevelError {
		t.Errorf("expected second=error, got %s", entries[1].Level)
	}
}

func TestConsoleRingBuffer_TimestampSet(t *testing.T) {
	r := NewConsoleRingBuffer(10)
	before := time.Now()
	r.Push(ConsoleEntry{
		Level: ConsoleLevelLog,
		Args:  []string{"test"},
	})
	after := time.Now()
	entries := r.Snapshot()
	if len(entries) != 1 {
		t.Fatal("expected 1 entry")
	}
	if entries[0].Timestamp.Before(before) || entries[0].Timestamp.After(after) {
		t.Errorf("timestamp not in expected range: %v", entries[0].Timestamp)
	}
}

func TestConsoleRingBuffer_TimestampPreserved(t *testing.T) {
	r := NewConsoleRingBuffer(10)
	ts := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	r.Push(ConsoleEntry{
		Level:     ConsoleLevelLog,
		Args:      []string{"test"},
		Timestamp: ts,
	})
	entries := r.Snapshot()
	if !entries[0].Timestamp.Equal(ts) {
		t.Errorf("expected timestamp %v, got %v", ts, entries[0].Timestamp)
	}
}

func TestConsoleRingBuffer_Overflow(t *testing.T) {
	r := NewConsoleRingBuffer(3)
	r.PushMessage(ConsoleLevelLog, []string{"1"})
	r.PushMessage(ConsoleLevelLog, []string{"2"})
	r.PushMessage(ConsoleLevelLog, []string{"3"})
	r.PushMessage(ConsoleLevelLog, []string{"4"})

	entries := r.Snapshot()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries (overflow), got %d", len(entries))
	}
	// Oldest should be "2" (1 was overwritten).
	if entries[0].Args[0] != "2" {
		t.Errorf("expected oldest=2, got %s", entries[0].Args[0])
	}
	if entries[2].Args[0] != "4" {
		t.Errorf("expected newest=4, got %s", entries[2].Args[0])
	}
}

func TestConsoleRingBuffer_IsFull(t *testing.T) {
	r := NewConsoleRingBuffer(3)
	if r.IsFull() {
		t.Error("expected not full initially")
	}
	r.PushMessage(ConsoleLevelLog, []string{"1"})
	r.PushMessage(ConsoleLevelLog, []string{"2"})
	r.PushMessage(ConsoleLevelLog, []string{"3"})
	if !r.IsFull() {
		t.Error("expected full after 3 pushes")
	}
}

func TestConsoleRingBuffer_Len(t *testing.T) {
	r := NewConsoleRingBuffer(10)
	if r.Len() != 0 {
		t.Fatalf("expected len=0, got %d", r.Len())
	}
	r.PushMessage(ConsoleLevelLog, []string{"1"})
	r.PushMessage(ConsoleLevelLog, []string{"2"})
	if r.Len() != 2 {
		t.Fatalf("expected len=2, got %d", r.Len())
	}
}

func TestConsoleRingBuffer_Len_Full(t *testing.T) {
	r := NewConsoleRingBuffer(3)
	r.PushMessage(ConsoleLevelLog, []string{"1"})
	r.PushMessage(ConsoleLevelLog, []string{"2"})
	r.PushMessage(ConsoleLevelLog, []string{"3"})
	r.PushMessage(ConsoleLevelLog, []string{"4"})
	if r.Len() != 3 {
		t.Fatalf("expected len=3 (cap), got %d", r.Len())
	}
}

func TestConsoleRingBuffer_Clear(t *testing.T) {
	r := NewConsoleRingBuffer(10)
	r.PushMessage(ConsoleLevelLog, []string{"1"})
	r.PushMessage(ConsoleLevelLog, []string{"2"})
	r.Clear()
	if r.Len() != 0 {
		t.Fatalf("expected len=0 after clear, got %d", r.Len())
	}
	if r.IsFull() {
		t.Error("expected not full after clear")
	}
}

func TestConsoleRingBuffer_SnapshotEmpty(t *testing.T) {
	r := NewConsoleRingBuffer(10)
	entries := r.Snapshot()
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestConsoleRingBuffer_FilterByLevel(t *testing.T) {
	r := NewConsoleRingBuffer(10)
	r.PushMessage(ConsoleLevelLog, []string{"log1"})
	r.PushMessage(ConsoleLevelError, []string{"err1"})
	r.PushMessage(ConsoleLevelWarn, []string{"warn1"})
	r.PushMessage(ConsoleLevelError, []string{"err2"})

	errors := r.FilterByLevel(ConsoleLevelError)
	if len(errors) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(errors))
	}
	if errors[0].Args[0] != "err1" {
		t.Errorf("expected first error=err1, got %s", errors[0].Args[0])
	}
	if errors[1].Args[0] != "err2" {
		t.Errorf("expected second error=err2, got %s", errors[1].Args[0])
	}
}

func TestConsoleRingBuffer_CountByLevel(t *testing.T) {
	r := NewConsoleRingBuffer(10)
	r.PushMessage(ConsoleLevelLog, []string{"1"})
	r.PushMessage(ConsoleLevelLog, []string{"2"})
	r.PushMessage(ConsoleLevelError, []string{"3"})
	r.PushMessage(ConsoleLevelWarn, []string{"4"})

	counts := r.CountByLevel()
	if counts[ConsoleLevelLog] != 2 {
		t.Errorf("expected 2 log, got %d", counts[ConsoleLevelLog])
	}
	if counts[ConsoleLevelError] != 1 {
		t.Errorf("expected 1 error, got %d", counts[ConsoleLevelError])
	}
	if counts[ConsoleLevelWarn] != 1 {
		t.Errorf("expected 1 warn, got %d", counts[ConsoleLevelWarn])
	}
}

func TestConsoleRingBuffer_Latest(t *testing.T) {
	r := NewConsoleRingBuffer(10)
	r.PushMessage(ConsoleLevelLog, []string{"first"})
	r.PushMessage(ConsoleLevelError, []string{"second"})

	latest := r.Latest()
	if latest == nil {
		t.Fatal("expected non-nil latest")
	}
	if latest.Args[0] != "second" {
		t.Errorf("expected latest=second, got %s", latest.Args[0])
	}
	if latest.Level != ConsoleLevelError {
		t.Errorf("expected latest level=error, got %s", latest.Level)
	}
}

func TestConsoleRingBuffer_Latest_Empty(t *testing.T) {
	r := NewConsoleRingBuffer(10)
	if r.Latest() != nil {
		t.Error("expected nil latest for empty buffer")
	}
}

func TestConsoleRingBuffer_Latest_AfterOverflow(t *testing.T) {
	r := NewConsoleRingBuffer(3)
	r.PushMessage(ConsoleLevelLog, []string{"1"})
	r.PushMessage(ConsoleLevelLog, []string{"2"})
	r.PushMessage(ConsoleLevelLog, []string{"3"})
	r.PushMessage(ConsoleLevelLog, []string{"4"})

	latest := r.Latest()
	if latest == nil {
		t.Fatal("expected non-nil latest")
	}
	if latest.Args[0] != "4" {
		t.Errorf("expected latest=4, got %s", latest.Args[0])
	}
}

func TestConsoleRingBuffer_DefaultBufferSizeIs1000(t *testing.T) {
	if DefaultConsoleBufferSize != 1000 {
		t.Fatalf("expected DefaultConsoleBufferSize=1000, got %d", DefaultConsoleBufferSize)
	}
}

func TestConsoleRingBuffer_FullCycle1000(t *testing.T) {
	r := NewConsoleRingBuffer(DefaultConsoleBufferSize)
	for i := 0; i < DefaultConsoleBufferSize; i++ {
		r.PushMessage(ConsoleLevelLog, []string{"msg"})
	}
	if !r.IsFull() {
		t.Error("expected full after 1000 pushes")
	}
	if r.Len() != DefaultConsoleBufferSize {
		t.Fatalf("expected len=1000, got %d", r.Len())
	}
	// Push one more to verify overflow.
	r.PushMessage(ConsoleLevelLog, []string{"overflow"})
	if r.Len() != DefaultConsoleBufferSize {
		t.Fatalf("expected len=1000 after overflow, got %d", r.Len())
	}
}

func TestConsoleRingBuffer_SnapshotDoesNotMutate(t *testing.T) {
	r := NewConsoleRingBuffer(10)
	r.PushMessage(ConsoleLevelLog, []string{"original"})
	snap := r.Snapshot()
	snap[0].Args[0] = "mutated"
	// Original buffer should not be affected.
	original := r.Snapshot()
	if original[0].Args[0] != "original" {
		t.Errorf("snapshot mutation leaked to original: %s", original[0].Args[0])
	}
}

func TestConsoleRingBuffer_ConcurrentPush(t *testing.T) {
	r := NewConsoleRingBuffer(1000)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				r.PushMessage(ConsoleLevelLog, []string{"concurrent"})
			}
		}()
	}
	wg.Wait()
	if r.Len() != 1000 {
		t.Fatalf("expected len=1000, got %d", r.Len())
	}
}

func TestConsoleRingBuffer_ConcurrentPushAndSnapshot(t *testing.T) {
	r := NewConsoleRingBuffer(1000)
	var wg sync.WaitGroup
	// Concurrent pushes.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				r.PushMessage(ConsoleLevelLog, []string{"concurrent"})
			}
		}()
	}
	// Concurrent snapshots.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = r.Snapshot()
			}
		}()
	}
	wg.Wait()
}

func TestConsoleRingBuffer_StackTrace(t *testing.T) {
	r := NewConsoleRingBuffer(10)
	r.Push(ConsoleEntry{
		Level:      ConsoleLevelError,
		Args:       []string{"error with trace"},
		StackTrace: "at foo:1:2\nat bar:3:4",
	})
	entries := r.Snapshot()
	if entries[0].StackTrace != "at foo:1:2\nat bar:3:4" {
		t.Errorf("expected stack trace preserved, got %s", entries[0].StackTrace)
	}
}

func TestConsoleRingBuffer_MultipleArgs(t *testing.T) {
	r := NewConsoleRingBuffer(10)
	r.PushMessage(ConsoleLevelLog, []string{"arg1", "arg2", "arg3"})
	entries := r.Snapshot()
	if len(entries[0].Args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(entries[0].Args))
	}
	if entries[0].Args[2] != "arg3" {
		t.Errorf("expected third arg=arg3, got %s", entries[0].Args[2])
	}
}

func TestConsoleRingBuffer_EmptyArgs(t *testing.T) {
	r := NewConsoleRingBuffer(10)
	r.PushMessage(ConsoleLevelLog, []string{})
	entries := r.Snapshot()
	if len(entries) != 1 {
		t.Fatal("expected 1 entry")
	}
	if len(entries[0].Args) != 0 {
		t.Errorf("expected 0 args, got %d", len(entries[0].Args))
	}
}
