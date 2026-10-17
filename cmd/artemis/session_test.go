package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/profile"
	"github.com/Christopher-Schulze/Artemis/serve"
)

func TestServeProcessOwnsProfileUntilShutdown(t *testing.T) {
	if port := os.Getenv("ARTEMIS_TEST_PROFILE_SERVE_PORT"); port != "" {
		os.Exit(cmdServe([]string{"--port", port, "--token", "profile-process-test", "--chromium=false", "--profile-root", os.Getenv("ARTEMIS_TEST_PROFILE_SERVE_ROOT")}))
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_, port, err := net.SplitHostPort(address)
	closeErr := listener.Close()
	if err := errors.Join(err, closeErr); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, executable, "-test.run=^TestServeProcessOwnsProfileUntilShutdown$")
	child.Env = append(os.Environ(), "ARTEMIS_TEST_PROFILE_SERVE_PORT="+port, "ARTEMIS_TEST_PROFILE_SERVE_ROOT="+root, "ARTEMIS_DIAGNOSTICS_FILE="+filepath.Join(t.TempDir(), "audit.jsonl"))
	var diagnostics bytes.Buffer
	child.Stderr = &diagnostics
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			cancel()
			if err := child.Wait(); err == nil {
				t.Error("unfinished server exited without expected cancellation")
			}
		}
	}()
	endpoint := "ws://" + address
	var client *serve.Client
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		attempt, cancelAttempt := context.WithTimeout(ctx, 100*time.Millisecond)
		client, err = serve.Dial(attempt, endpoint, "profile-process-test", "")
		cancelAttempt()
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if client == nil {
		t.Fatalf("real Serve did not become ready: %v", err)
	}
	identity := client.ClientID()
	var opened serve.SessionNewResult
	callErr := client.Call(ctx, serve.CmdSessionNew, serve.SessionNewParams{ProfileID: "shutdown-profile", Class: "persistent"}, &opened)
	if err := errors.Join(callErr, client.Close()); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, "locks", "shutdown-profile.lock")
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("Serve did not retain profile lock after disconnect: %v", err)
	}
	code, raw := captureCLIOutput(t, func() int {
		return cmdSession([]string{"--server", endpoint, "--token", "profile-process-test", "--client-id", identity})
	})
	var listed struct {
		Result serve.SessionListResult `json:"result"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil || code != 0 || len(listed.Result.Sessions) != 1 || listed.Result.Sessions[0].SessionID != opened.SessionID || !listed.Result.Sessions[0].Active {
		t.Fatalf("real Serve lost session after disconnect: %s code=%d err=%v", raw, code, err)
	}
	if err := child.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	err = child.Wait()
	waited = true
	if err != nil {
		t.Fatalf("Serve shutdown failed: %v %s", err, diagnostics.String())
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Serve shutdown leaked profile lock: %v", err)
	}
	manager, err := profile.NewRuntimeManager(root)
	if err != nil {
		t.Fatal(err)
	}
	state, err := manager.Get(profile.SessionID(opened.SessionID), opened.OwnerUserRef)
	if err != nil || state.State != profile.SessionClosed || state.CrashMarker || state.Recovered {
		t.Fatalf("graceful Serve exit left a crashed session: %+v err=%v", state, err)
	}
}

func TestSessionRejectsInvalidAndIgnoredFlagsBeforeConnection(t *testing.T) {
	t.Setenv("ARTEMIS_CLIENT_ID", "")
	var hits atomic.Int64
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer peer.Close()
	endpoint := "ws" + strings.TrimPrefix(peer.URL, "http")
	for _, args := range [][]string{
		{"--op", "invalid"}, {"--timeout", "0"}, {"extra"},
		{"--op", "open", "--class", "invalid"}, {"--op", "open", "--class", "persistent"},
		{"--op", "open", "--class", "attached"}, {"--op", "open", "--id", "ignored"},
		{"--op", "open", "--session", ""}, {"--op", "open", "--page", ""}, {"--op", "open", "--snapshot-epoch", "0"},
		{"--op", "list"}, {"--op", "list", "--client-id", "client", "--profile", "ignored"},
		{"--op", "list", "--client-id", "client", "--class", "ephemeral"},
		{"--op", "list", "--client-id", "client", "--id", "ignored"},
		{"--op", "close"}, {"--op", "close", "--id", "session"},
		{"--op", "close", "--id", "session", "--client-id", "client", "--profile", "ignored"},
		{"--root", t.TempDir()}, {"--owner", "alice"},
	} {
		if code := cmdSession(append([]string{"--server", endpoint, "--token", "token"}, args...)); code != 2 || hits.Load() != 0 {
			t.Fatalf("%v reached peer or returned wrong code: %d hits=%d", args, code, hits.Load())
		}
	}
	if code := cmdSession([]string{"--server", "", "--op", "open"}); code != 2 {
		t.Fatalf("missing server returned %d", code)
	}
}

func TestSessionOutputFailurePreservesActualEffectAndConnectionCleanup(t *testing.T) {
	t.Setenv("ARTEMIS_CLIENT_ID", "")
	agent, err := artemis.NewAgent(artemis.AgentConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestResource(t, "agent stop", agent.Stop) })
	server := serve.New(agent, serve.Opts{AuthToken: "session-output-test", RateLimit: serve.RateLimit{RequestsPerSecond: 1000, Burst: 1000, ClientRequestsPerMinute: 60000, ClientBurst: 1000}})
	t.Cleanup(func() { closeTestResource(t, "server close", server.Close) })
	var connections atomic.Int64
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections.Add(1)
		defer connections.Add(-1)
		server.HandleWSForTest(w, r)
	}))
	defer host.Close()
	args := []string{"--server", "ws" + strings.TrimPrefix(host.URL, "http"), "--token", "session-output-test"}
	code, raw := captureCLIOutput(t, func() int { return cmdSession(append(args, "--op", "open")) })
	var opened struct {
		ClientID string                 `json:"clientId"`
		Result   serve.SessionNewResult `json:"result"`
	}
	if err := json.Unmarshal(raw, &opened); err != nil || code != 0 || opened.ClientID == "" || opened.Result.OwnerUserRef == "" || opened.Result.SessionID == "" {
		t.Fatalf("actual session result missing: %s code=%d err=%v", raw, code, err)
	}
	ownedArgs := append(append([]string(nil), args...), "--client-id", opened.ClientID)
	closed, err := os.Create(filepath.Join(t.TempDir(), "closed"))
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"list", "close"} {
		operation := append(append([]string(nil), ownedArgs...), "--op", op)
		if op == "close" {
			operation = append(operation, "--id", opened.Result.SessionID)
		}
		func() {
			original := os.Stdout
			os.Stdout = closed
			defer func() { os.Stdout = original }()
			if code := cmdSession(operation); code != 1 {
				t.Fatalf("%s ignored failed output: code=%d", op, code)
			}
		}()
		_, active := agent.Session(opened.Result.SessionID)
		if active != (op == "list") {
			t.Fatalf("%s output failure hid or changed its actual effect: active=%v", op, active)
		}
		deadline := time.Now().Add(time.Second)
		for connections.Load() != 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if connections.Load() != 0 {
			t.Fatalf("%s leaked its connection after failed output", op)
		}
	}
	code, raw = captureCLIOutput(t, func() int { return cmdSession(ownedArgs) })
	var listed struct {
		Result serve.SessionListResult `json:"result"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil || code != 0 || len(listed.Result.Sessions) != 0 {
		t.Fatalf("closed session survived reconnect: %s code=%d err=%v", raw, code, err)
	}
}
