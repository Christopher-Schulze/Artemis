package actions

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge/cdpops"
)

// TestNavigatorWaitConditionsFollowRealChromiumNetworkLifecycle proves each
// wait condition against one real timeline: the document loads at once while
// a single fetch stays in flight for two seconds.
func TestNavigatorWaitConditionsFollowRealChromiumNetworkLifecycle(t *testing.T) {
	fixture := newActionFixture(t)
	mux, ok := fixture.server.Config.Handler.(*http.ServeMux)
	if !ok {
		t.Fatal("action fixture no longer serves through a ServeMux")
	}
	var finished sync.Map
	mux.HandleFunc("/idle-page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if _, err := fmt.Fprintf(w, `<!doctype html><title>Idle</title><script>fetch('/slow-fetch?id=%s')</script>`, r.URL.Query().Get("id")); err != nil {
			t.Errorf("write idle page: %v", err)
		}
	})
	mux.HandleFunc("/slow-fetch", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
			return
		}
		finished.Store(r.URL.Query().Get("id"), true)
		if _, err := w.Write([]byte("done")); err != nil {
			t.Errorf("write slow fetch: %v", err)
		}
	})
	navigator := cdpops.NewNavigator(pageCaller{page: fixture.page})
	for _, tc := range []struct {
		condition    cdpops.WaitCondition
		fetchSettled bool
	}{
		{cdpops.WaitLoad, false},
		{cdpops.WaitNetworkAlmostIdle, false},
		{cdpops.WaitNetworkIdle, true},
	} {
		id := string(tc.condition)
		result := navigator.Navigate(t.Context(), cdpops.NavigationRequest{URL: fixture.server.URL + "/idle-page?id=" + id, WaitUntil: tc.condition, Timeout: 15 * time.Second})
		_, settled := finished.Load(id)
		if !result.Success || settled != tc.fetchSettled {
			t.Fatalf("%s did not follow the real network timeline: result=%+v fetchSettled=%t want=%t", tc.condition, result, settled, tc.fetchSettled)
		}
	}
	// The page is idle now; WaitForLoad must see Chrome's replayed current-document state.
	start := time.Now()
	if err := navigator.WaitForLoad(t.Context(), cdpops.WaitNetworkIdle, 5*time.Second); err != nil || time.Since(start) > 2*time.Second {
		t.Fatalf("current-document networkIdle was not replayed by real Chromium: %v after %s", err, time.Since(start))
	}
}
