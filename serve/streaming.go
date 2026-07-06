package serve

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// StreamEventType labels the payload kind of a StreamEvent.
type StreamEventType string

const (
	StreamEventFrame    StreamEventType = "frame"
	StreamEventSnapshot StreamEventType = "snapshot"
	StreamEventMetrics  StreamEventType = "metrics"
	StreamEventLog      StreamEventType = "log"
	StreamEventError    StreamEventType = "error"
)

// FrameMetadata describes the viewport state associated with a streamed frame.
type FrameMetadata struct {
	ScrollX        int64   `json:"scrollX"`
	ScrollY        int64   `json:"scrollY"`
	ViewportWidth  int64   `json:"viewportWidth"`
	ViewportHeight int64   `json:"viewportHeight"`
	Scale          float64 `json:"scale"`
	Timestamp      int64   `json:"timestamp"`
}

// StreamEvent is a single server-pushed streaming event (spec L4267).
type StreamEvent struct {
	Type          StreamEventType `json:"type"`
	Data          json.RawMessage `json:"data,omitempty"`
	Timestamp     int64           `json:"timestamp"`
	FrameMetadata *FrameMetadata  `json:"frameMetadata,omitempty"`
}

// StreamSubscriber receives streamed events.
type StreamSubscriber interface {
	OnEvent(event StreamEvent)
}

// StreamingConfig controls the streaming WebSocket server port selection.
type StreamingConfig struct {
	BasePort   int
	MaxRetries int
	Enabled    bool
}

// DefaultStreamingConfig is the canonical streaming configuration.
var DefaultStreamingConfig = StreamingConfig{
	BasePort:   8765,
	MaxRetries: 5,
	Enabled:    true,
}

// StreamingServer fans out StreamEvents to registered subscribers and
// optionally binds a TCP listener with port fallback.
type StreamingServer struct {
	mu           sync.RWMutex
	subscribers  map[int]StreamSubscriber
	nextID       int
	listener     net.Listener
	port         int
	config       StreamingConfig
	broadcastSeq atomic.Int64
}

// NewStreamingServer builds a StreamingServer with the given config.
func NewStreamingServer(config StreamingConfig) *StreamingServer {
	if config.BasePort == 0 {
		config = DefaultStreamingConfig
	}
	return &StreamingServer{
		subscribers: make(map[int]StreamSubscriber),
		config:      config,
	}
}

// Subscribe registers a subscriber and returns its id. The id is used to
// unsubscribe later.
func (s *StreamingServer) Subscribe(subscriber StreamSubscriber) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := s.nextID
	s.subscribers[id] = subscriber
	return id
}

// Unsubscribe removes a subscriber by id. Returns true if removed.
func (s *StreamingServer) Unsubscribe(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.subscribers[id]; !ok {
		return false
	}
	delete(s.subscribers, id)
	return true
}

// GetSubscriberCount returns the current number of subscribers.
func (s *StreamingServer) GetSubscriberCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.subscribers)
}

// Broadcast delivers an event to every subscriber. Subscribers are invoked
// without holding the lock so a slow subscriber cannot block others; each
// delivery happens in its own goroutine.
//
// Optimization (TASK-2344): fast-path the common cases of 0 and 1
// subscriber to avoid goroutine spawn + WaitGroup overhead. The 0-
// subscriber path still counts the broadcast attempt but skips the
// timestamp computation and slice allocation.
func (s *StreamingServer) Broadcast(event StreamEvent) {
	s.mu.RLock()
	n := len(s.subscribers)
	if n == 0 {
		s.mu.RUnlock()
		// Still count the broadcast attempt so GetBroadcastCount
		// reflects all Broadcast calls, not just delivered ones.
		s.broadcastSeq.Add(1)
		return
	}
	// 1-subscriber fast path: call directly, no goroutine, no WaitGroup.
	if n == 1 {
		var single StreamSubscriber
		for _, sub := range s.subscribers {
			single = sub
		}
		s.mu.RUnlock()
		if event.Timestamp == 0 {
			event.Timestamp = time.Now().UnixMilli()
		}
		s.broadcastSeq.Add(1)
		single.OnEvent(event)
		return
	}
	// 2+ subscribers: snapshot under RLock, fan out in goroutines.
	subs := make([]StreamSubscriber, 0, n)
	for _, sub := range s.subscribers {
		subs = append(subs, sub)
	}
	s.mu.RUnlock()

	if event.Timestamp == 0 {
		event.Timestamp = time.Now().UnixMilli()
	}
	s.broadcastSeq.Add(1)

	var wg sync.WaitGroup
	wg.Add(len(subs))
	for _, sub := range subs {
		go func(sub StreamSubscriber) {
			defer wg.Done()
			sub.OnEvent(event)
		}(sub)
	}
	wg.Wait()
}

// GetBroadcastCount returns the total number of broadcasts issued.
func (s *StreamingServer) GetBroadcastCount() int64 {
	return s.broadcastSeq.Load()
}

// Start binds a TCP listener with port fallback up to MaxRetries. Returns
// the chosen port.
func (s *StreamingServer) Start() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.port, nil
	}
	if !s.config.Enabled {
		return 0, fmt.Errorf("streaming disabled in config")
	}
	base := s.config.BasePort
	max := s.config.MaxRetries
	if max < 0 {
		max = 0
	}
	var lastErr error
	for attempt := 0; attempt <= max; attempt++ {
		port := base + attempt
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			lastErr = err
			continue
		}
		s.listener = ln
		s.port = port
		return port, nil
	}
	return 0, fmt.Errorf("no available port in range %d-%d: %w", base, base+max, lastErr)
}

// Stop closes the listener if present.
func (s *StreamingServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	err := s.listener.Close()
	s.listener = nil
	s.port = 0
	return err
}

// Port returns the currently bound port, or 0 if not listening.
func (s *StreamingServer) Port() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.port
}

// NewFrameMetadata builds a FrameMetadata with the current timestamp.
func NewFrameMetadata(scrollX, scrollY, vpW, vpH int64, scale float64) FrameMetadata {
	return FrameMetadata{
		ScrollX:        scrollX,
		ScrollY:        scrollY,
		ViewportWidth:  vpW,
		ViewportHeight: vpH,
		Scale:          scale,
		Timestamp:      time.Now().UnixMilli(),
	}
}

// NewFrameEvent builds a StreamEvent of type frame with metadata.
func NewFrameEvent(data json.RawMessage, meta FrameMetadata) StreamEvent {
	return StreamEvent{
		Type:          StreamEventFrame,
		Data:          data,
		Timestamp:     time.Now().UnixMilli(),
		FrameMetadata: &meta,
	}
}

// EncodeEvent serializes a StreamEvent to JSON.
func EncodeEvent(event StreamEvent) ([]byte, error) {
	return json.Marshal(event)
}

// DecodeEvent deserializes a StreamEvent from JSON.
func DecodeEvent(raw []byte) (StreamEvent, error) {
	var e StreamEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return StreamEvent{}, err
	}
	return e, nil
}

// PortString returns the bound port as a string suitable for display.
func (s *StreamingServer) PortString() string {
	return strconv.Itoa(s.Port())
}
