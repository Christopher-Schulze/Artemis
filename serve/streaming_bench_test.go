package serve

import (
	"encoding/json"
	"testing"
)

// noopSubscriber is a StreamSubscriber that does nothing — used to
// benchmark the Broadcast fan-out path without measuring subscriber
// work.
type noopSubscriber struct{}

func (noopSubscriber) OnEvent(StreamEvent) {}

// BenchmarkBroadcastNoSubscribers measures the Broadcast hot
// path with zero subscribers (the common idle case).
func BenchmarkBroadcastNoSubscribers(b *testing.B) {
	s := NewStreamingServer(DefaultStreamingConfig)
	evt := StreamEvent{Type: StreamEventFrame, Data: json.RawMessage(`{}`)}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Broadcast(evt)
	}
}

// BenchmarkBroadcastOneSubscriber measures the Broadcast hot
// path with one subscriber (the typical active case).
func BenchmarkBroadcastOneSubscriber(b *testing.B) {
	s := NewStreamingServer(DefaultStreamingConfig)
	s.Subscribe(noopSubscriber{})
	evt := StreamEvent{Type: StreamEventFrame, Data: json.RawMessage(`{}`)}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Broadcast(evt)
	}
}

// BenchmarkBroadcastFiveSubscribers measures the Broadcast
// hot path with five subscribers (high-fan-out case).
func BenchmarkBroadcastFiveSubscribers(b *testing.B) {
	s := NewStreamingServer(DefaultStreamingConfig)
	for i := 0; i < 5; i++ {
		s.Subscribe(noopSubscriber{})
	}
	evt := StreamEvent{Type: StreamEventFrame, Data: json.RawMessage(`{}`)}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Broadcast(evt)
	}
}

// BenchmarkEncodeEvent measures the EncodeEvent hot path
// (JSON serialization of a StreamEvent — called per broadcast).
func BenchmarkEncodeEvent(b *testing.B) {
	evt := StreamEvent{
		Type:      StreamEventFrame,
		Data:      json.RawMessage(`{"url":"https://example.com","title":"Test"}`),
		Timestamp: 1234567890,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := EncodeEvent(evt); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecodeEvent measures the DecodeEvent hot path
// (JSON deserialization of a StreamEvent — called per incoming event).
func BenchmarkDecodeEvent(b *testing.B) {
	raw := []byte(`{"type":"frame","data":{"url":"https://example.com"},"timestamp":1234567890}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodeEvent(raw); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkNewFrameEvent measures the NewFrameEvent hot path
// (called per frame event construction).
func BenchmarkNewFrameEvent(b *testing.B) {
	data := json.RawMessage(`{"url":"https://example.com"}`)
	meta := FrameMetadata{ScrollX: 0, ScrollY: 0, ViewportWidth: 1920, ViewportHeight: 1080, Scale: 1.0}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewFrameEvent(data, meta)
	}
}
