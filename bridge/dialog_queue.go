package bridge

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// DialogEventType identifies the kind of buffered dialog event (spec L4321).
type DialogEventType string

const (
	// DialogEventDialog is a JS dialog (alert/confirm/prompt/beforeunload).
	DialogEventDialog DialogEventType = "dialog"
	// DialogEventFileChooser is a file-chooser invocation.
	DialogEventFileChooser DialogEventType = "file_chooser"
)

// DialogEvent is a buffered JS dialog event waiting for agent processing.
type DialogEvent struct {
	Type          DialogEventType
	Message       string
	DefaultPrompt string
	URL           string
	Timestamp     time.Time
}

// FileChooserEvent is a buffered file-chooser event waiting for agent
// processing.
type FileChooserEvent struct {
	URL       string
	Multiple  bool
	Timestamp time.Time
}

// DialogQueueConfig controls dialog/file-chooser queuing (spec L4321).
type DialogQueueConfig struct {
	Enabled      bool
	MaxQueueSize int
}

// DefaultDialogQueueConfig is the canonical dialog-queue configuration.
var DefaultDialogQueueConfig = DialogQueueConfig{
	Enabled:      true,
	MaxQueueSize: 100,
}

// DialogQueueStats reports counters for the queue.
type DialogQueueStats struct {
	TotalDialogs      int64
	TotalFileChoosers int64
	Dequeued          int64
	Dropped           int64
}

// DialogQueue buffers async dialog and file-chooser events for later agent
// processing so no events are lost (spec L4321). Events are returned in FIFO
// order. The queue is thread-safe.
type DialogQueue struct {
	mu     sync.Mutex
	config DialogQueueConfig
	events []interface{}
	stats  DialogQueueStats
	now    func() time.Time
}

// NewDialogQueue builds a DialogQueue with the supplied config. Zero-value
// MaxQueueSize falls back to DefaultDialogQueueConfig.MaxQueueSize.
func NewDialogQueue(config DialogQueueConfig) *DialogQueue {
	if config.MaxQueueSize <= 0 {
		config.MaxQueueSize = DefaultDialogQueueConfig.MaxQueueSize
	}
	return &DialogQueue{
		config: config,
		now:    time.Now,
	}
}

// EnqueueDialog buffers a dialog event. Returns an error if the queue is
// disabled or full; a full queue drops the event and increments Dropped.
func (q *DialogQueue) EnqueueDialog(message, defaultPrompt, url string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.config.Enabled {
		return errors.New("dialog queue: disabled")
	}
	if len(q.events) >= q.config.MaxQueueSize {
		q.stats.Dropped++
		return fmt.Errorf("dialog queue: full (max=%d)", q.config.MaxQueueSize)
	}
	q.events = append(q.events, DialogEvent{
		Type:          DialogEventDialog,
		Message:       message,
		DefaultPrompt: defaultPrompt,
		URL:           url,
		Timestamp:     q.now(),
	})
	q.stats.TotalDialogs++
	return nil
}

// EnqueueFileChooser buffers a file-chooser event. Returns an error if the
// queue is disabled or full; a full queue drops the event and increments
// Dropped.
func (q *DialogQueue) EnqueueFileChooser(url string, multiple bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.config.Enabled {
		return errors.New("dialog queue: disabled")
	}
	if len(q.events) >= q.config.MaxQueueSize {
		q.stats.Dropped++
		return fmt.Errorf("dialog queue: full (max=%d)", q.config.MaxQueueSize)
	}
	q.events = append(q.events, FileChooserEvent{
		URL:       url,
		Multiple:  multiple,
		Timestamp: q.now(),
	})
	q.stats.TotalFileChoosers++
	return nil
}

// Dequeue returns the next buffered event (DialogEvent or FileChooserEvent)
// in FIFO order, or an error if the queue is empty.
func (q *DialogQueue) Dequeue() (interface{}, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.events) == 0 {
		return nil, errors.New("dialog queue: empty")
	}
	ev := q.events[0]
	q.events = q.events[1:]
	q.stats.Dequeued++
	return ev, nil
}

// DequeueDialog returns the next dialog event only, scanning the queue in
// FIFO order and removing/returning the first DialogEvent. File-chooser
// events remain in the queue. Returns an error if no dialog event exists.
func (q *DialogQueue) DequeueDialog() (*DialogEvent, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, ev := range q.events {
		if d, ok := ev.(DialogEvent); ok {
			q.events = append(q.events[:i], q.events[i+1:]...)
			q.stats.Dequeued++
			dcopy := d
			return &dcopy, nil
		}
	}
	return nil, errors.New("dialog queue: no dialog event")
}

// DequeueFileChooser returns the next file-chooser event only, scanning the
// queue in FIFO order and removing/returning the first FileChooserEvent.
// Dialog events remain in the queue. Returns an error if no file-chooser
// event exists.
func (q *DialogQueue) DequeueFileChooser() (*FileChooserEvent, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, ev := range q.events {
		if f, ok := ev.(FileChooserEvent); ok {
			q.events = append(q.events[:i], q.events[i+1:]...)
			q.stats.Dequeued++
			fcopy := f
			return &fcopy, nil
		}
	}
	return nil, errors.New("dialog queue: no file chooser event")
}

// Len returns the total number of queued events.
func (q *DialogQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.events)
}

// IsEmpty reports whether the queue has no buffered events.
func (q *DialogQueue) IsEmpty() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.events) == 0
}

// Clear removes all buffered events. Returns the number of events cleared.
func (q *DialogQueue) Clear() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := len(q.events)
	q.events = q.events[:0]
	return n
}

// Stats returns a snapshot of the queue counters.
func (q *DialogQueue) Stats() DialogQueueStats {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.stats
}

// Config returns a copy of the queue configuration.
func (q *DialogQueue) Config() DialogQueueConfig {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.config
}
