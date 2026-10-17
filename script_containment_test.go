package artemis

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/engine"
	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func containmentServer(t *testing.T) (*httptest.Server, *atomic.Int32, int) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/spin" {
			fmt.Fprint(w, `<!doctype html><title>spin</title><script>for(;;){}</script>`)
			return
		}
		fmt.Fprint(w, `<!doctype html><title>static</title><p id="out">server</p><script>document.getElementById('out').textContent='script ran'</script>`)
	}))
	t.Cleanup(server.Close)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	return server, &hits, port
}

func startContainmentAgent(t *testing.T, port int, chromium bool, contained bool) *Agent {
	t.Helper()
	config := AgentConfig{PolicyEngine: fixtureActionPolicy(t), PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}}, RequireScriptContainment: contained}
	if chromium {
		config.Chromium = &browserprocess.LaunchConfig{Headless: true}
	}
	agent, err := NewAgent(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := agent.Stop(); err != nil {
			t.Error(err)
		}
	})
	return agent
}

func TestEngineRefusesPageScriptsUnderContainmentBeforeAnyRequest(t *testing.T) {
	server, hits, port := containmentServer(t)
	contained, err := engine.New(engine.Config{RequireScriptContainment: true, PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := contained.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, opts := range []engine.FetchOpts{{RunScripts: true}, {RunInlineScripts: true}, {AsyncFetch: true}} {
		if _, err := contained.Fetch(t.Context(), server.URL, opts); !errors.Is(err, engine.ErrScriptContainmentRequired) {
			t.Fatalf("%+v: err = %v", opts, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("refused fetch reached the network %d times", hits.Load())
	}
	page, err := contained.Fetch(t.Context(), server.URL, engine.FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := page.Close(); err != nil {
			t.Error(err)
		}
	}()
	if !strings.Contains(page.Text(), "server") || strings.Contains(page.Text(), "script ran") {
		t.Fatalf("script-free fetch executed page JavaScript: %q", page.Text())
	}
}

func TestContainedAgentWithoutChromiumFailsExplicitly(t *testing.T) {
	server, _, port := containmentServer(t)
	agent := startContainmentAgent(t, port, false, true)
	session, err := agent.CreateSession("contained")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, taskErr := session.OpenPage(t.Context(), server.URL, true); taskErr == nil || taskErr.Code != TaskErrorCapabilityUnavailable {
		t.Fatalf("scripted renderless page opened under containment: %v", taskErr)
	}
	pageID, page, taskErr := session.OpenPage(t.Context(), server.URL, false)
	if taskErr != nil || page == nil || strings.Contains(page.Text(), "script ran") {
		t.Fatalf("script-free renderless page: %v", taskErr)
	}
	if taskErr := session.ClosePage(pageID); taskErr != nil {
		t.Fatal(taskErr)
	}
	result := agent.ExecuteTask(t.Context(), Task{ID: "js", SessionID: session.SessionID(), Action: FetchAction{URL: server.URL, RunScripts: true}})
	if result.Success || result.ErrorCode != TaskErrorCapabilityUnavailable {
		t.Fatalf("scripted task without a contained runtime: %+v", result)
	}
}

func TestContainedAgentRunsScriptsOnlyInChromiumAndSurvivesHostileJS(t *testing.T) {
	server, _, port := containmentServer(t)
	agent := startContainmentAgent(t, port, true, true)
	session, err := agent.CreateSession("contained")
	if err != nil {
		t.Fatal(err)
	}
	result := agent.ExecuteTask(t.Context(), Task{ID: "js", SessionID: session.SessionID(), Action: FetchAction{URL: server.URL, RunScripts: true}})
	if !result.Success || result.Data == nil || !strings.Contains(result.Data.Text, "script ran") {
		t.Fatalf("contained route did not execute the script in Chromium: %+v %s %s", result, result.ErrorCode, result.Error)
	}
	if _, _, taskErr := session.OpenPage(t.Context(), server.URL, true); taskErr == nil || taskErr.Code != TaskErrorCapabilityUnavailable {
		t.Fatalf("in-process scripted page opened next to Chromium: %v", taskErr)
	}

	started := time.Now()
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	hostile := agent.ExecuteTask(ctx, Task{ID: "spin", SessionID: session.SessionID(), Action: FetchAction{URL: server.URL + "/spin", RunScripts: true}, Timeout: 4 * time.Second})
	if hostile.Success || time.Since(started) > 30*time.Second {
		t.Fatalf("infinite page script was not bounded: %+v after %s", hostile, time.Since(started))
	}
	recovered := agent.ExecuteTask(t.Context(), Task{ID: "after", SessionID: session.SessionID(), Action: FetchAction{URL: server.URL, RunScripts: true}})
	if !recovered.Success || !strings.Contains(recovered.Data.Text, "script ran") {
		t.Fatalf("contained runtime did not recover after hostile JS: %+v %s %s (hostile: %s %s)", recovered, recovered.ErrorCode, recovered.Error, hostile.ErrorCode, hostile.Error)
	}

	direct := startContainmentAgent(t, port, false, false)
	directSession, err := direct.CreateSession("direct")
	if err != nil {
		t.Fatal(err)
	}
	_, page, taskErr := directSession.OpenPage(t.Context(), server.URL, true)
	if taskErr != nil || !strings.Contains(page.Text(), "script ran") {
		t.Fatalf("direct mode lost in-process script execution: %v", taskErr)
	}
}
