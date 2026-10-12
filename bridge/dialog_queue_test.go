package bridge

import (
	"sync"
	"testing"
)

func TestDefaultDialogQueueConfig(t *testing.T) {
	c := DefaultDialogQueueConfig
	if !c.Enabled {
		t.Fatal("default should be enabled")
	}
	if c.MaxQueueSize != 100 {
		t.Fatalf("maxQueueSize=%d want 100", c.MaxQueueSize)
	}
}

func TestNewDialogQueueDefaultsMaxSize(t *testing.T) {
	q := NewDialogQueue(DialogQueueConfig{Enabled: true})
	if got := q.Config().MaxQueueSize; got != 100 {
		t.Fatalf("maxQueueSize=%d want 100", got)
	}
}

func TestDialogQueueEnqueueDialog(t *testing.T) {
	q := NewDialogQueue(DefaultDialogQueueConfig)
	if err := q.EnqueueDialog("are you sure?", "ok", "https://example.com"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if q.Len() != 1 {
		t.Fatalf("len=%d want 1", q.Len())
	}
	s := q.Stats()
	if s.TotalDialogs != 1 {
		t.Fatalf("totalDialogs=%d want 1", s.TotalDialogs)
	}
}

func TestDialogQueueEnqueueFileChooser(t *testing.T) {
	q := NewDialogQueue(DefaultDialogQueueConfig)
	if err := q.EnqueueFileChooser("https://example.com/upload", true); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if q.Len() != 1 {
		t.Fatalf("len=%d want 1", q.Len())
	}
	s := q.Stats()
	if s.TotalFileChoosers != 1 {
		t.Fatalf("totalFileChoosers=%d want 1", s.TotalFileChoosers)
	}
}

func TestDialogQueueDequeueDialog(t *testing.T) {
	q := NewDialogQueue(DefaultDialogQueueConfig)
	_ = q.EnqueueDialog("hello", "default", "https://example.com")
	ev, err := q.Dequeue()
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	d, ok := ev.(DialogEvent)
	if !ok {
		t.Fatalf("expected DialogEvent, got %T", ev)
	}
	if d.Message != "hello" {
		t.Fatalf("message=%q want hello", d.Message)
	}
	if d.DefaultPrompt != "default" {
		t.Fatalf("defaultPrompt=%q want default", d.DefaultPrompt)
	}
	if d.URL != "https://example.com" {
		t.Fatalf("url=%q", d.URL)
	}
	if d.Type != DialogEventDialog {
		t.Fatalf("type=%q want dialog", d.Type)
	}
	if !q.IsEmpty() {
		t.Fatal("queue should be empty after dequeue")
	}
}

func TestDialogQueueDequeueFileChooser(t *testing.T) {
	q := NewDialogQueue(DefaultDialogQueueConfig)
	_ = q.EnqueueFileChooser("https://example.com/upload", true)
	ev, err := q.Dequeue()
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	f, ok := ev.(FileChooserEvent)
	if !ok {
		t.Fatalf("expected FileChooserEvent, got %T", ev)
	}
	if !f.Multiple {
		t.Fatal("multiple should be true")
	}
	if f.URL != "https://example.com/upload" {
		t.Fatalf("url=%q", f.URL)
	}
}

func TestDialogQueueDequeueEmpty(t *testing.T) {
	q := NewDialogQueue(DefaultDialogQueueConfig)
	if _, err := q.Dequeue(); err == nil {
		t.Fatal("expected error on empty dequeue")
	}
}

func TestDialogQueueDequeueDialogOnly(t *testing.T) {
	q := NewDialogQueue(DefaultDialogQueueConfig)
	_ = q.EnqueueFileChooser("https://example.com/fc", false)
	_ = q.EnqueueDialog("confirm?", "", "https://example.com/d")
	d, err := q.DequeueDialog()
	if err != nil {
		t.Fatalf("dequeueDialog: %v", err)
	}
	if d.Message != "confirm?" {
		t.Fatalf("message=%q want confirm?", d.Message)
	}
	// File chooser should remain.
	if q.Len() != 1 {
		t.Fatalf("len=%d want 1 (file chooser remains)", q.Len())
	}
}

func TestDialogQueueDequeueDialogNone(t *testing.T) {
	q := NewDialogQueue(DefaultDialogQueueConfig)
	_ = q.EnqueueFileChooser("https://example.com/fc", false)
	if _, err := q.DequeueDialog(); err == nil {
		t.Fatal("expected error when no dialog event")
	}
}

func TestDialogQueueDequeueFileChooserOnly(t *testing.T) {
	q := NewDialogQueue(DefaultDialogQueueConfig)
	_ = q.EnqueueDialog("confirm?", "", "https://example.com/d")
	_ = q.EnqueueFileChooser("https://example.com/fc", true)
	f, err := q.DequeueFileChooser()
	if err != nil {
		t.Fatalf("dequeueFileChooser: %v", err)
	}
	if !f.Multiple {
		t.Fatal("multiple should be true")
	}
	// Dialog should remain.
	if q.Len() != 1 {
		t.Fatalf("len=%d want 1 (dialog remains)", q.Len())
	}
}

func TestDialogQueueDequeueFileChooserNone(t *testing.T) {
	q := NewDialogQueue(DefaultDialogQueueConfig)
	_ = q.EnqueueDialog("confirm?", "", "https://example.com/d")
	if _, err := q.DequeueFileChooser(); err == nil {
		t.Fatal("expected error when no file chooser event")
	}
}

func TestDialogQueueFIFOOrder(t *testing.T) {
	q := NewDialogQueue(DefaultDialogQueueConfig)
	_ = q.EnqueueDialog("first", "", "u1")
	_ = q.EnqueueFileChooser("u2", false)
	_ = q.EnqueueDialog("third", "", "u3")
	first, _ := q.Dequeue()
	second, _ := q.Dequeue()
	third, _ := q.Dequeue()
	if d, ok := first.(DialogEvent); !ok || d.Message != "first" {
		t.Fatalf("first should be dialog first, got %v", first)
	}
	if _, ok := second.(FileChooserEvent); !ok {
		t.Fatalf("second should be file chooser, got %T", second)
	}
	if d, ok := third.(DialogEvent); !ok || d.Message != "third" {
		t.Fatalf("third should be dialog third, got %v", third)
	}
}

func TestDialogQueueMaxSize(t *testing.T) {
	q := NewDialogQueue(DialogQueueConfig{Enabled: true, MaxQueueSize: 2})
	_ = q.EnqueueDialog("a", "", "u")
	_ = q.EnqueueDialog("b", "", "u")
	err := q.EnqueueDialog("c", "", "u")
	if err == nil {
		t.Fatal("expected error when queue full")
	}
	if q.Len() != 2 {
		t.Fatalf("len=%d want 2", q.Len())
	}
	s := q.Stats()
	if s.Dropped != 1 {
		t.Fatalf("dropped=%d want 1", s.Dropped)
	}
}

func TestDialogQueueDisabled(t *testing.T) {
	q := NewDialogQueue(DialogQueueConfig{Enabled: false, MaxQueueSize: 10})
	if err := q.EnqueueDialog("a", "", "u"); err == nil {
		t.Fatal("expected error when disabled")
	}
	if err := q.EnqueueFileChooser("u", true); err == nil {
		t.Fatal("expected error when disabled")
	}
	if q.Len() != 0 {
		t.Fatalf("len=%d want 0", q.Len())
	}
}

func TestDialogQueueClear(t *testing.T) {
	q := NewDialogQueue(DefaultDialogQueueConfig)
	_ = q.EnqueueDialog("a", "", "u")
	_ = q.EnqueueFileChooser("u", true)
	n := q.Clear()
	if n != 2 {
		t.Fatalf("cleared=%d want 2", n)
	}
	if !q.IsEmpty() {
		t.Fatal("queue should be empty after clear")
	}
}

func TestDialogQueueClearEmpty(t *testing.T) {
	q := NewDialogQueue(DefaultDialogQueueConfig)
	if n := q.Clear(); n != 0 {
		t.Fatalf("cleared=%d want 0", n)
	}
}

func TestDialogQueueIsEmpty(t *testing.T) {
	q := NewDialogQueue(DefaultDialogQueueConfig)
	if !q.IsEmpty() {
		t.Fatal("new queue should be empty")
	}
	_ = q.EnqueueDialog("a", "", "u")
	if q.IsEmpty() {
		t.Fatal("queue should not be empty after enqueue")
	}
}

func TestDialogQueueStats(t *testing.T) {
	q := NewDialogQueue(DefaultDialogQueueConfig)
	_ = q.EnqueueDialog("a", "", "u")
	_ = q.EnqueueFileChooser("u", true)
	_ = q.EnqueueDialog("b", "", "u")
	_, _ = q.Dequeue()
	s := q.Stats()
	if s.TotalDialogs != 2 {
		t.Fatalf("totalDialogs=%d want 2", s.TotalDialogs)
	}
	if s.TotalFileChoosers != 1 {
		t.Fatalf("totalFileChoosers=%d want 1", s.TotalFileChoosers)
	}
	if s.Dequeued != 1 {
		t.Fatalf("dequeued=%d want 1", s.Dequeued)
	}
	if s.Dropped != 0 {
		t.Fatalf("dropped=%d want 0", s.Dropped)
	}
}

func TestDialogQueueNoEventsLost(t *testing.T) {
	q := NewDialogQueue(DefaultDialogQueueConfig)
	const n = 20
	for i := 0; i < n; i++ {
		if err := q.EnqueueDialog("m", "", "u"); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	got := 0
	for {
		_, err := q.Dequeue()
		if err != nil {
			break
		}
		got++
	}
	if got != n {
		t.Fatalf("dequeued=%d want %d (events lost)", got, n)
	}
}

func TestDialogQueueConcurrent(t *testing.T) {
	q := NewDialogQueue(DialogQueueConfig{Enabled: true, MaxQueueSize: 1000})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = q.EnqueueDialog("m", "", "u")
			_ = q.EnqueueFileChooser("u", true)
			_, _ = q.Dequeue()
			_ = q.Len()
			_ = q.IsEmpty()
		}(i)
	}
	wg.Wait()
	s := q.Stats()
	if s.TotalDialogs+s.TotalFileChoosers == 0 {
		t.Fatal("expected some enqueues under concurrency")
	}
}
