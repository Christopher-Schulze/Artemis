package serve

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type captureSubscriber struct {
	mu     sync.Mutex
	events []StreamEvent
}

func (c *captureSubscriber) OnEvent(e StreamEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *captureSubscriber) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

func TestSubscribeReturnsID(t *testing.T) {
	s := NewStreamingServer(DefaultStreamingConfig)
	id1 := s.Subscribe(&captureSubscriber{})
	id2 := s.Subscribe(&captureSubscriber{})
	if id1 == id2 {
		t.Fatalf("ids must differ: %d == %d", id1, id2)
	}
	if s.GetSubscriberCount() != 2 {
		t.Fatalf("count=%d", s.GetSubscriberCount())
	}
}

func TestUnsubscribe(t *testing.T) {
	s := NewStreamingServer(DefaultStreamingConfig)
	id := s.Subscribe(&captureSubscriber{})
	if !s.Unsubscribe(id) {
		t.Fatal("unsubscribe should return true")
	}
	if s.GetSubscriberCount() != 0 {
		t.Fatalf("count=%d", s.GetSubscriberCount())
	}
	if s.Unsubscribe(id) {
		t.Fatal("second unsubscribe should return false")
	}
}

func TestUnsubscribeUnknownID(t *testing.T) {
	s := NewStreamingServer(DefaultStreamingConfig)
	if s.Unsubscribe(999) {
		t.Fatal("unknown id should not remove anything")
	}
}

func TestBroadcastSingleSubscriber(t *testing.T) {
	s := NewStreamingServer(DefaultStreamingConfig)
	c := &captureSubscriber{}
	s.Subscribe(c)
	s.Broadcast(StreamEvent{Type: StreamEventFrame, Data: json.RawMessage(`"hello"`)})
	if c.count() != 1 {
		t.Fatalf("count=%d", c.count())
	}
}

func TestBroadcastMultipleSubscribers(t *testing.T) {
	s := NewStreamingServer(DefaultStreamingConfig)
	a, b := &captureSubscriber{}, &captureSubscriber{}
	s.Subscribe(a)
	s.Subscribe(b)
	s.Broadcast(StreamEvent{Type: StreamEventSnapshot})
	if a.count() != 1 || b.count() != 1 {
		t.Fatalf("a=%d b=%d", a.count(), b.count())
	}
}

func TestBroadcastNoSubscribers(t *testing.T) {
	s := NewStreamingServer(DefaultStreamingConfig)
	s.Broadcast(StreamEvent{Type: StreamEventFrame})
	if s.GetBroadcastCount() != 1 {
		t.Fatalf("broadcast count=%d", s.GetBroadcastCount())
	}
}

func TestBroadcastSetsTimestamp(t *testing.T) {
	s := NewStreamingServer(DefaultStreamingConfig)
	c := &captureSubscriber{}
	s.Subscribe(c)
	s.Broadcast(StreamEvent{Type: StreamEventFrame})
	if c.events[0].Timestamp == 0 {
		t.Fatal("timestamp should be set")
	}
}

func TestBroadcastPreservesTimestamp(t *testing.T) {
	s := NewStreamingServer(DefaultStreamingConfig)
	c := &captureSubscriber{}
	s.Subscribe(c)
	s.Broadcast(StreamEvent{Type: StreamEventFrame, Timestamp: 12345})
	if c.events[0].Timestamp != 12345 {
		t.Fatalf("timestamp=%d", c.events[0].Timestamp)
	}
}

func TestBroadcastCount(t *testing.T) {
	s := NewStreamingServer(DefaultStreamingConfig)
	c := &captureSubscriber{}
	s.Subscribe(c)
	for i := 0; i < 5; i++ {
		s.Broadcast(StreamEvent{Type: StreamEventFrame})
	}
	if s.GetBroadcastCount() != 5 {
		t.Fatalf("count=%d", s.GetBroadcastCount())
	}
	if c.count() != 5 {
		t.Fatalf("received=%d", c.count())
	}
}

func TestStartBindsPort(t *testing.T) {
	cfg := StreamingConfig{BasePort: 18765, MaxRetries: 3, Enabled: true}
	s := NewStreamingServer(cfg)
	port, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	if port != 18765 {
		t.Fatalf("port=%d", port)
	}
	defer s.Stop()
	if s.Port() != port {
		t.Fatalf("Port()=%d", s.Port())
	}
}

func TestStartPortFallback(t *testing.T) {
	// Occupy the base port first.
	blocker := NewStreamingServer(StreamingConfig{BasePort: 18780, MaxRetries: 0, Enabled: true})
	if _, err := blocker.Start(); err != nil {
		t.Fatal(err)
	}
	defer blocker.Stop()

	s := NewStreamingServer(StreamingConfig{BasePort: 18780, MaxRetries: 3, Enabled: true})
	port, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	if port == 18780 {
		t.Fatalf("expected fallback port, got %d", port)
	}
}

func TestStartDisabled(t *testing.T) {
	s := NewStreamingServer(StreamingConfig{BasePort: 18790, MaxRetries: 1, Enabled: false})
	if _, err := s.Start(); err == nil {
		t.Fatal("expected error when disabled")
	}
}

func TestStartIdempotent(t *testing.T) {
	s := NewStreamingServer(StreamingConfig{BasePort: 18800, MaxRetries: 1, Enabled: true})
	p1, _ := s.Start()
	p2, _ := s.Start()
	if p1 != p2 {
		t.Fatalf("p1=%d p2=%d", p1, p2)
	}
	defer s.Stop()
}

func TestStopWithoutStart(t *testing.T) {
	s := NewStreamingServer(DefaultStreamingConfig)
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestNewFrameMetadata(t *testing.T) {
	m := NewFrameMetadata(10, 20, 1280, 720, 1.5)
	if m.ScrollX != 10 || m.ScrollY != 20 || m.ViewportWidth != 1280 || m.ViewportHeight != 720 || m.Scale != 1.5 {
		t.Fatalf("meta=%+v", m)
	}
	if m.Timestamp == 0 {
		t.Fatal("timestamp should be set")
	}
}

func TestNewFrameEvent(t *testing.T) {
	meta := NewFrameMetadata(0, 0, 100, 100, 1.0)
	e := NewFrameEvent(json.RawMessage(`"frame1"`), meta)
	if e.Type != StreamEventFrame {
		t.Fatalf("type=%s", e.Type)
	}
	if e.FrameMetadata == nil || e.FrameMetadata.ViewportWidth != 100 {
		t.Fatalf("meta=%+v", e.FrameMetadata)
	}
}

func TestEncodeDecodeEvent(t *testing.T) {
	meta := NewFrameMetadata(1, 2, 3, 4, 2.0)
	e := NewFrameEvent(json.RawMessage(`"x"`), meta)
	raw, err := EncodeEvent(e)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeEvent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != StreamEventFrame {
		t.Fatalf("type=%s", got.Type)
	}
	if got.FrameMetadata == nil || got.FrameMetadata.ScrollX != 1 {
		t.Fatalf("meta=%+v", got.FrameMetadata)
	}
}

func TestPortString(t *testing.T) {
	s := NewStreamingServer(StreamingConfig{BasePort: 18810, MaxRetries: 0, Enabled: true})
	if _, err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	if s.PortString() != "18810" {
		t.Fatalf("portString=%q", s.PortString())
	}
}

func TestConcurrentBroadcast(t *testing.T) {
	s := NewStreamingServer(DefaultStreamingConfig)
	var received atomic.Int64
	sub := &countingSub{received: &received}
	s.Subscribe(sub)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Broadcast(StreamEvent{Type: StreamEventFrame})
		}()
	}
	wg.Wait()
	if received.Load() != 10 {
		t.Fatalf("received=%d", received.Load())
	}
}

type countingSub struct {
	received *atomic.Int64
}

func (c *countingSub) OnEvent(e StreamEvent) {
	c.received.Add(1)
}

func TestDefaultStreamingConfigValues(t *testing.T) {
	if DefaultStreamingConfig.BasePort != 8765 {
		t.Fatalf("basePort=%d", DefaultStreamingConfig.BasePort)
	}
	if DefaultStreamingConfig.MaxRetries != 5 {
		t.Fatalf("maxRetries=%d", DefaultStreamingConfig.MaxRetries)
	}
	if !DefaultStreamingConfig.Enabled {
		t.Fatal("should be enabled")
	}
}

func TestNewStreamingServerZeroConfig(t *testing.T) {
	s := NewStreamingServer(StreamingConfig{})
	if s.config.BasePort != DefaultStreamingConfig.BasePort {
		t.Fatalf("basePort=%d", s.config.BasePort)
	}
}

func TestBroadcastTimestampRecent(t *testing.T) {
	s := NewStreamingServer(DefaultStreamingConfig)
	c := &captureSubscriber{}
	s.Subscribe(c)
	before := time.Now().UnixMilli()
	s.Broadcast(StreamEvent{Type: StreamEventFrame})
	after := time.Now().UnixMilli()
	ts := c.events[0].Timestamp
	if ts < before || ts > after+10 {
		t.Fatalf("timestamp=%d before=%d after=%d", ts, before, after)
	}
}
