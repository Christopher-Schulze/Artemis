package tabs

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

type recyclingSource struct {
	targets  map[string]Target
	recycled []string
	fail     error
	next     int
}

func (s *recyclingSource) ListTargets(context.Context) ([]Target, error) {
	out := make([]Target, 0, len(s.targets))
	for _, target := range s.targets {
		out = append(out, target)
	}
	return out, nil
}

func (s *recyclingSource) CreateTarget(_ context.Context, url string) (Target, error) {
	s.next++
	id := string(rune('a' + s.next))
	target := Target{ID: id, URL: url, State: "attached", CreatedAt: time.Unix(int64(1000+s.next), 0)}
	s.targets[id] = target
	return target, nil
}

func (s *recyclingSource) ActivateTarget(context.Context, string) error { return nil }

func (s *recyclingSource) CloseTarget(_ context.Context, id string) error {
	delete(s.targets, id)
	return nil
}

func (s *recyclingSource) RecycleTarget(_ context.Context, id string) error {
	if s.fail != nil {
		return s.fail
	}
	target := s.targets[id]
	target.URL = blankURL
	s.targets[id] = target
	s.recycled = append(s.recycled, id)
	return nil
}

func TestRecycleTickRecyclesOnlyIdlePoolTabsAndHonorsUnlimitedHeap(t *testing.T) {
	source := &recyclingSource{targets: map[string]Target{"session": {ID: "session", URL: "https://s.example/", State: "attached", CreatedAt: time.Unix(1, 0)}, "root": {ID: "root", URL: "https://r.example/", State: "active", CreatedAt: time.Unix(1, 0)}}}
	registry := NewTabRegistry(source)
	first, err := registry.CreateTabContext(context.Background(), "owner", "https://a.example/")
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.CreateTabContext(context.Background(), "owner", "https://b.example/")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000+60*60, 0)
	unlimited := RecyclePolicy{IdleAfter: 30 * time.Minute, Now: func() time.Time { return now }, HeapAlloc: func() uint64 { return math.MaxUint64 }, HeapLimit: func() int64 { return math.MaxInt64 }}
	recycled, err := registry.RecycleTick(context.Background(), unlimited)
	if err != nil || len(recycled) != 2 || len(source.recycled) != 2 {
		t.Fatalf("idle pool tabs = %v %v", recycled, err)
	}
	for _, id := range source.recycled {
		if id == "session" || id == "root" {
			t.Fatalf("non-pool target %s recycled", id)
		}
	}
	if again, err := registry.RecycleTick(context.Background(), unlimited); err != nil || len(again) != 0 {
		t.Fatalf("blank tabs recycled again: %v %v", again, err)
	}
	if registry.RecycledTotal() != 2 {
		t.Fatalf("recycled total = %d", registry.RecycledTotal())
	}
	if _, err := registry.CloseTabContext(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	source.fail = errors.New("cdp gone")
	third, err := registry.CreateTabContext(context.Background(), "owner", "https://c.example/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RecycleTick(context.Background(), unlimited); err == nil {
		t.Fatal("a failed recycle was not reported")
	}
	if tab, _ := registry.GetTab(third.ID); tab.URL == blankURL || second.ID == "" {
		t.Fatalf("a failed recycle marked the tab blank: %+v", tab)
	}
	if _, err := NewTabRegistry(&partialSource{}).RecycleTick(context.Background(), unlimited); err == nil {
		t.Fatal("recycling through a source without recycle support succeeded")
	}
}

type partialSource struct{}

func (partialSource) ListTargets(context.Context) ([]Target, error)        { return nil, nil }
func (partialSource) CreateTarget(context.Context, string) (Target, error) { return Target{}, nil }
func (partialSource) ActivateTarget(context.Context, string) error         { return nil }
func (partialSource) CloseTarget(context.Context, string) error            { return nil }
