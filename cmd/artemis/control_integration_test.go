package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	"github.com/Christopher-Schulze/Artemis/observe"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/profile"
	"github.com/Christopher-Schulze/Artemis/security"
	"github.com/Christopher-Schulze/Artemis/serve"
)

func TestCLIControlPersistsAcrossProcessesAndRejectsAnotherClient(t *testing.T) {
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		writeTestBody(t, w, `<!doctype html><title>Persistent CLI</title><label>Name <input></label><button onclick="document.querySelector('p').textContent=document.querySelector('input').value">Save</button><p>waiting</p>`)
	}))
	defer fixture.Close()
	policyPath := filepath.Join(t.TempDir(), "action-policy.json")
	if err := os.WriteFile(policyPath, []byte(`{"default":"allow"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	agent, err := artemis.NewAgent(artemis.AgentConfig{ActionPolicyPath: policyPath, Chromium: &browserprocess.LaunchConfig{Headless: true}, PolicyConfig: testConfig(fixture).PolicyConfig})
	if err != nil {
		t.Fatal(err)
	}
	profileRoot := t.TempDir()
	manager, err := profile.NewRuntimeManager(profileRoot)
	if err != nil {
		t.Fatal(err)
	}
	agent.SetProfileRuntime(manager)
	if err := agent.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestResource(t, "agent stop", agent.Stop) })
	server := serve.New(agent, serve.Opts{AuthToken: "cli-control-test", RateLimit: serve.RateLimit{RequestsPerSecond: 1000, Burst: 1000, ClientRequestsPerMinute: 60000, ClientBurst: 1000}})
	t.Cleanup(func() { closeTestResource(t, "control server close", server.Close) })
	host := httptest.NewServer(http.HandlerFunc(server.HandleWSForTest))
	defer host.Close()
	endpoint := "ws" + strings.TrimPrefix(host.URL, "http") + "/"
	binary := filepath.Join(t.TempDir(), "artemis")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real CLI: %v\n%s", err, output)
	}
	invoke := func(command string, flags ...string) []byte {
		t.Helper()
		args := append([]string{command, "--server", endpoint, "--token", "cli-control-test"}, flags...)
		output, err := exec.CommandContext(t.Context(), binary, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("CLI %s failed: %v\n%s", command, err, output)
		}
		return output
	}
	var opened struct {
		ClientID string                 `json:"clientId"`
		Result   serve.SessionNewResult `json:"result"`
	}
	if err := json.Unmarshal(invoke("session", "--op", "open", "--profile", "cli-profile", "--class", "persistent"), &opened); err != nil {
		t.Fatal(err)
	}
	if opened.ClientID == "" || opened.Result.SessionID == "" {
		t.Fatalf("missing identity: %+v", opened)
	}
	var listed struct {
		ClientID string                  `json:"clientId"`
		Result   serve.SessionListResult `json:"result"`
	}
	if err := json.Unmarshal(invoke("session", "--client-id", opened.ClientID), &listed); err != nil || listed.ClientID != opened.ClientID || len(listed.Result.Sessions) != 1 || !listed.Result.Sessions[0].Active || listed.Result.Sessions[0].SessionID != opened.Result.SessionID {
		t.Fatalf("session did not survive its creating CLI process: %+v err=%v", listed, err)
	}
	state, err := manager.Get(profile.SessionID(opened.Result.SessionID), opened.Result.OwnerUserRef)
	if err != nil || state.State != profile.SessionActive || state.Class != profile.ProfilePersistent {
		t.Fatalf("server did not retain managed session: %+v err=%v", state, err)
	}
	lockPath := filepath.Join(profileRoot, "locks", "cli-profile.lock")
	lock, err := os.ReadFile(lockPath)
	if err != nil || strings.TrimSpace(string(lock)) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("profile lock is not held by the surviving server: %s err=%v", lock, err)
	}
	if _, err := manager.Open(t.Context(), profile.OpenSessionRequest{ProfileID: "cli-profile", OwnerUserRef: opened.Result.OwnerUserRef, Class: profile.ProfilePersistent}); err == nil {
		t.Fatal("active persistent profile admitted a second owner")
	}
	params, err := json.Marshal(serve.ChromiumOpenParams{SessionID: opened.Result.SessionID, URL: fixture.URL})
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Result serve.ChromiumOpenResult `json:"result"`
		Trust  *security.ContentTrust   `json:"trust"`
	}
	if err := json.Unmarshal(invoke("control", "--client-id", opened.ClientID, "--cmd", "chromium.open", "--params", string(params)), &page); err != nil {
		t.Fatal(err)
	}
	origin := strings.TrimSuffix(fixture.URL, "/")
	requireCLITrust := func(name string, output []byte, source string) {
		t.Helper()
		var labeled struct {
			Trust *security.ContentTrust `json:"trust"`
		}
		if err := json.Unmarshal(output, &labeled); err != nil || labeled.Trust == nil || labeled.Trust.Origin != origin || labeled.Trust.Source != source || labeled.Trust.Trust != security.TrustUntrusted {
			t.Fatalf("%s output lacks its page trust label: %+v err=%v", name, labeled.Trust, err)
		}
	}
	if page.Trust == nil || page.Trust.Origin != origin || page.Trust.Source != "chromium.open" {
		t.Fatalf("control chromium.open lacks its trust label: %+v", page.Trust)
	}
	common := []string{"--client-id", opened.ClientID, "--session", opened.Result.SessionID, "--page", page.Result.PageID}
	var before observe.ObservationEvidence
	observed := invoke("observe", common...)
	if err := json.Unmarshal(observed, &before); err != nil {
		t.Fatal(err)
	}
	requireCLITrust("observe", observed, "observe")
	queryArgs := append(append([]string{}, common...), "--roles", "textbox,button", "--max-bytes", "8192")
	var queried bridgeobserve.QueryResult
	queryOutput := invoke("query", queryArgs...)
	if err := json.Unmarshal(queryOutput, &queried); err != nil || queried.Kind != bridgeobserve.QueryFull || queried.Snapshot == nil || len(queryOutput) > 8192 {
		t.Fatalf("real CLI query failed its projection/budget: %+v bytes=%d err=%v", queried, len(queryOutput), err)
	}
	requireCLITrust("query", queryOutput, "query")
	var unchanged bridgeobserve.QueryResult
	if err := json.Unmarshal(invoke("query", append(queryArgs, "--since", queried.Revision)...), &unchanged); err != nil || unchanged.Kind != bridgeobserve.QueryUnchanged || unchanged.Revision != queried.Revision || unchanged.Snapshot != nil {
		t.Fatalf("query baseline did not persist across real CLI processes: %+v err=%v", unchanged, err)
	}
	input, button := "", ""
	for _, node := range before.Snapshot.Nodes {
		if node.Tag == "input" {
			input = node.Ref
		}
		if node.Role == "button" {
			button = node.Ref
		}
	}
	if input == "" || button == "" {
		t.Fatalf("missing refs: %+v", before.Snapshot.Nodes)
	}
	for _, request := range []actions.Request{{Kind: actions.KindFill, Ref: input, Text: "persisted-value"}, {Kind: actions.KindClick, Ref: button}} {
		encoded, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		args := append(append([]string{}, common...), "--snapshot-epoch", strconv.FormatUint(before.Snapshot.Epoch, 10), "--request", string(encoded))
		var outcome actions.Outcome
		acted := invoke("act", args...)
		if err := json.Unmarshal(acted, &outcome); err != nil || !outcome.Success {
			t.Fatalf("real action failed: %+v %v", outcome, err)
		}
		requireCLITrust("act", acted, "act")
	}
	dumpArgs := append(append([]string{}, common...), "--dump", "text")
	if output := string(invoke("fetch", dumpArgs...)); !strings.Contains(output, "persisted-value") {
		t.Fatalf("effect lost across processes: %s", output)
	}
	if output := string(invoke("fetch", append(dumpArgs, "--fence")...)); !strings.Contains(output, `origin="`+origin+`" source="fetch" trust="untrusted"`) || !strings.Contains(output, "persisted-value") {
		t.Fatalf("persistent fetch --fence did not fence page text: %s", output)
	}
	other, err := serve.Dial(t.Context(), endpoint, "cli-control-test", "")
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "other client close", other.Close)
	if err := json.Unmarshal(invoke("session", "--client-id", other.ClientID()), &listed); err != nil || len(listed.Result.Sessions) != 0 {
		t.Fatalf("another client listed the owner's session: %+v err=%v", listed, err)
	}
	foreignClose := exec.CommandContext(t.Context(), binary, "session", "--server", endpoint, "--token", "cli-control-test", "--client-id", other.ClientID(), "--op", "close", "--id", opened.Result.SessionID)
	if output, err := foreignClose.CombinedOutput(); err == nil || !strings.Contains(string(output), string(serve.ErrOwnershipDenied)) {
		t.Fatalf("another client closed the owner's session: %v %s", err, output)
	}
	if err := other.Call(t.Context(), serve.CmdChromiumQuery, serve.ChromiumQueryParams{SessionID: opened.Result.SessionID, PageID: page.Result.PageID}, &unchanged); err == nil || !strings.Contains(err.Error(), string(serve.ErrOwnershipDenied)) {
		t.Fatalf("another client inherited query access: %v", err)
	}
	var result observe.ObservationEvidence
	if err := other.Call(t.Context(), serve.CmdChromiumObserve, serve.ChromiumObserveParams{SessionID: opened.Result.SessionID, PageID: page.Result.PageID}, &result); err == nil || !strings.Contains(err.Error(), string(serve.ErrOwnershipDenied)) {
		t.Fatalf("another client inherited control: %v", err)
	}
	owner, err := serve.Dial(t.Context(), endpoint, "cli-control-test", opened.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "owner client close", owner.Close)
	if err := owner.Call(t.Context(), serve.CmdChromiumQuery, json.RawMessage(`{"sessionId":"`+opened.Result.SessionID+`","pageId":"`+page.Result.PageID+`","request":{"options":{"rooles":["button"]}}}`), &unchanged); err == nil || !strings.Contains(err.Error(), string(serve.ErrBadParams)) {
		t.Fatalf("unknown query option silently ignored: %v", err)
	}
	if err := owner.Call(t.Context(), serve.CmdChromiumQuery, json.RawMessage(`null`), &unchanged); err == nil || !strings.Contains(err.Error(), string(serve.ErrBadParams)) {
		t.Fatalf("non-object query parameters admitted: %v", err)
	}
	invoke("session", "--client-id", opened.ClientID, "--op", "close", "--id", opened.Result.SessionID)
	state, err = manager.Get(profile.SessionID(opened.Result.SessionID), opened.Result.OwnerUserRef)
	if err != nil || state.State != profile.SessionClosed || state.CrashMarker {
		t.Fatalf("actual close did not settle the server runtime: %+v err=%v", state, err)
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("actual close retained the persistent profile lock: %v", err)
	}
	reopened, err := manager.Open(t.Context(), profile.OpenSessionRequest{ProfileID: "cli-profile", OwnerUserRef: opened.Result.OwnerUserRef, Class: profile.ProfilePersistent})
	if err != nil {
		t.Fatalf("closed profile could not be acquired again: %v", err)
	}
	if err := manager.Close(t.Context(), reopened.ID, opened.Result.OwnerUserRef); err != nil {
		t.Fatal(err)
	}
	if err := owner.Call(t.Context(), serve.CmdChromiumObserve, serve.ChromiumObserveParams{SessionID: opened.Result.SessionID, PageID: page.Result.PageID}, &result); err == nil {
		t.Fatal("closed session remained accessible")
	}
	t.Logf("real CLI processes preserve page, refs, fill/click outcome and authenticated reconnect; protocol=%s time=%s", serve.ProtocolVersion, time.Now().UTC().Format(time.RFC3339))
}

func TestPersistentCommandsRejectIgnoredLocalFlags(t *testing.T) {
	for _, command := range []struct {
		name  string
		run   func([]string) int
		flags []string
	}{
		{"observe", cmdObserve, []string{"--binary", "ignored"}},
		{"act", cmdAct, []string{"--request", `{"kind":"screenshot"}`, "--sandbox", "disabled"}},
		{"fetch", cmdFetch, []string{"--allow-private-networks"}},
		{"query epoch", cmdQuery, []string{"--snapshot-epoch", "0"}},
		{"query byte budget", cmdQuery, []string{"--max-bytes", "1"}},
	} {
		t.Run(command.name, func(t *testing.T) {
			args := append([]string{"--server", "ws://127.0.0.1:1/", "--session", "s", "--page", "p"}, command.flags...)
			if code := command.run(args); code != 2 {
				t.Fatalf("ignored flags admitted: code=%d", code)
			}
		})
	}
}
