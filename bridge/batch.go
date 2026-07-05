package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Command is a single CDP command waiting to be sent.
type Command struct {
	ID     int64
	Method string
	Params map[string]interface{}
}

// Response is the result of a CDP command.
type Response struct {
	ID     int64
	Result json.RawMessage
	Error  *Error
}

// Error mirrors the Chrome DevTools Protocol error shape.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("cdp error %d: %s", e.Code, e.Message) }

// Batcher collects CDP commands and flushes them in a single batch.
// It is safe for concurrent use by a single goroutine; callers should
// hold the mutex while adding commands and while reading responses.
type Batcher struct {
	mu      sync.Mutex
	pending []Command
	nextID  int64
	flusher Flusher
	timeout time.Duration
	maxSize int
}

// Flusher is the transport-dependent function that actually sends the
// batch over the wire.  A real implementation (e.g. chromedp) fills this
// in; the unit tests use an in-memory mock.
type Flusher func(ctx context.Context, cmds []Command) ([]Response, error)

// NewBatcher creates a Batcher with the given flush function.
// maxSize is the maximum number of commands that will be accumulated
// before an automatic flush happens (0 = no limit).
func NewBatcher(f Flusher, maxSize int) *Batcher {
	return &Batcher{
		flusher: f,
		timeout: 30 * time.Second,
		maxSize: maxSize,
	}
}

// Add queues a command and returns its assigned ID.  If the batch
// reaches maxSize the pending set is flushed immediately.
func (b *Batcher) Add(ctx context.Context, method string, params map[string]interface{}) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	id := b.nextID
	b.pending = append(b.pending, Command{ID: id, Method: method, Params: params})

	if b.maxSize > 0 && len(b.pending) >= b.maxSize {
		return id, b.flushLocked(ctx)
	}
	return id, nil
}

// Flush sends all pending commands in one batch.
func (b *Batcher) Flush(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.flushLocked(ctx)
}

func (b *Batcher) flushLocked(ctx context.Context) error {
	if len(b.pending) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	batch := make([]Command, len(b.pending))
	copy(batch, b.pending)
	b.pending = b.pending[:0]

	_, err := b.flusher(ctx, batch)
	return err
}

// Pending returns the number of commands currently buffered.
func (b *Batcher) Pending() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}

// --- Form batch helpers ---

// FormFill is one field in a batch form fill operation.
type FormFill struct {
	Selector string
	Value    string
}

// BatchFillForm builds CDP Input.insertText / DOM.setAttributeValue
// commands for every field and adds them to the batcher.  It returns
// the command IDs so callers can correlate responses.
func BatchFillForm(b *Batcher, ctx context.Context, fields []FormFill) ([]int64, error) {
	ids := make([]int64, 0, len(fields)*2)
	for _, f := range fields {
		// Focus
		id, err := b.Add(ctx, "DOM.focus", map[string]interface{}{
			"selector": f.Selector,
		})
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)

		// Insert text
		id, err = b.Add(ctx, "Input.insertText", map[string]interface{}{
			"text": f.Value,
		})
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}
