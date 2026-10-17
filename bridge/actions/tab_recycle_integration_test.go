package actions

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	artemistabs "github.com/Christopher-Schulze/Artemis/bridge/tabs"
)

func contextPage(t *testing.T, root *bridge.Page, id string) *bridge.Page {
	t.Helper()
	for _, page := range root.ContextPages() {
		if page.TargetID() == id {
			return page
		}
	}
	t.Fatalf("no context page %s", id)
	return nil
}

func evaluateOn(t *testing.T, ctx context.Context, page *bridge.Page, expression string) json.RawMessage {
	t.Helper()
	var result runtimeCallResult
	if err := page.Call(ctx, "Runtime.evaluate", runtimeEvaluateParams{Expression: expression, ReturnByValue: true}, &result); err != nil {
		t.Fatal(err)
	}
	var value json.RawMessage
	if err := result.valueInto(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func openPoolTab(t *testing.T, ctx context.Context, runtime *Runtime, url string) string {
	t.Helper()
	opened := runtime.Execute(ctx, Request{Kind: KindTabOpen, URL: url})
	if !opened.Success || opened.TargetID == "" {
		t.Fatalf("tab open = %+v", opened)
	}
	return opened.TargetID
}

func TestPoolTabRecyclingClearsOriginDataAndKeepsTheTabReusable(t *testing.T) {
	fixture := newActionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	second := fixture.server.URL + "/second"
	poolID := openPoolTab(t, ctx, fixture.runtime, second)
	pool := contextPage(t, fixture.page, poolID)
	evaluateOn(t, ctx, pool, `document.cookie="recycle=1; path=/"; localStorage.setItem("recycle","1"); true`)

	// A session page opened outside the runtime shares the browser context but
	// is not a pool tab: recycling must never touch it.
	session, err := fixture.page.NewSibling(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := session.Close(); err != nil {
			t.Error(err)
		}
	}()

	recycled, err := fixture.runtime.tabs.RecycleTick(ctx, artemistabs.RecyclePolicy{IdleAfter: time.Nanosecond})
	if err != nil || len(recycled) != 1 || recycled[0] != poolID {
		t.Fatalf("idle recycle = %v %v", recycled, err)
	}
	if url := string(evaluateOn(t, ctx, pool, "location.href")); url != `"about:blank"` {
		t.Fatalf("recycled tab is at %s", url)
	}
	if url := string(evaluateOn(t, ctx, session, "location.href")); url != `"`+second+`"` {
		t.Fatalf("a session page was recycled: %s", url)
	}
	if url := string(evaluateOn(t, ctx, fixture.page, "location.href")); url == `"about:blank"` {
		t.Fatal("the runtime's active page was recycled")
	}
	var cookies struct {
		Cookies []struct {
			Name string `json:"name"`
		} `json:"cookies"`
	}
	if err := fixture.page.Call(ctx, "Network.getCookies", map[string]any{"urls": []string{second}}, &cookies); err != nil {
		t.Fatal(err)
	}
	for _, cookie := range cookies.Cookies {
		if cookie.Name == "recycle" {
			t.Fatal("recycling kept the origin cookie")
		}
	}
	if _, _, err := pool.Navigate(ctx, second); err != nil {
		t.Fatalf("recycled tab is not reusable: %v", err)
	}
	if stored := string(evaluateOn(t, ctx, pool, `localStorage.getItem("recycle")`)); stored != "null" {
		t.Fatalf("recycling kept localStorage: %s", stored)
	}
	if fixture.runtime.tabs.RecycledTotal() != 1 {
		t.Fatalf("recycled total = %d", fixture.runtime.tabs.RecycledTotal())
	}

	// Heap pressure recycles the least recently active pool tab even before
	// the idle timeout.
	evaluateOn(t, ctx, pool, "true")
	newer := openPoolTab(t, ctx, fixture.runtime, second)
	fixture.runtime.tabs.RecordActivity(newer, time.Now().Add(time.Minute))
	pressure := artemistabs.RecyclePolicy{IdleAfter: time.Hour, HeapAlloc: func() uint64 { return 900 }, HeapLimit: func() int64 { return 1000 }}
	recycled, err = fixture.runtime.tabs.RecycleTick(ctx, pressure)
	if err != nil || len(recycled) != 1 || recycled[0] != poolID {
		t.Fatalf("pressure recycle = %v %v (want the older pool tab %s)", recycled, err, poolID)
	}
	calm := pressure
	calm.HeapAlloc = func() uint64 { return 100 }
	if recycled, err := fixture.runtime.tabs.RecycleTick(ctx, calm); err != nil || len(recycled) != 0 {
		t.Fatalf("recycle without idle or pressure = %v %v", recycled, err)
	}
}

func TestTabRecycleSupervisorStopsWithItsRuntime(t *testing.T) {
	fixture := newActionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	runtime, err := NewRuntimeWithConfig(fixture.page, fixture.observer, nil, RuntimeConfig{
		DefaultTimeout: 30 * time.Second, UserID: "supervised",
		TabRecycle: artemistabs.RecyclePolicy{IdleAfter: 20 * time.Millisecond, Interval: 10 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	openPoolTab(t, ctx, runtime, fixture.server.URL+"/second")
	deadline := time.Now().Add(10 * time.Second)
	for runtime.tabs.RecycledTotal() == 0 && time.Now().Before(deadline) {
		timer := time.NewTimer(20 * time.Millisecond)
		<-timer.C
	}
	if runtime.tabs.RecycledTotal() == 0 {
		t.Fatalf("supervisor never recycled the idle tab: %s", runtime.tabs.LastRecycleError())
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	created, err := runtime.tabs.CreateTabContext(ctx, "supervised", fixture.server.URL+"/second")
	if err != nil {
		t.Fatal(err)
	}
	before := runtime.tabs.RecycledTotal()
	timer := time.NewTimer(300 * time.Millisecond)
	<-timer.C
	if runtime.tabs.RecycledTotal() != before {
		t.Fatal("the supervisor kept running after its runtime closed")
	}
	if tab, ok := runtime.tabs.GetTab(created.ID); !ok || tab.URL == "about:blank" {
		t.Fatalf("a tab was recycled after close: %+v", tab)
	}
}
