package smoke

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestSmokeActualNetworkAndProductOutcomes(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refused := "http://" + listener.Addr().String() + "/"
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, url, mode string
		want            Outcome
		skipped         bool
	}{
		{"tolerant refused socket", refused, "tolerant", OutcomeNetworkSkip, true},
		{"strict refused socket", refused, "strict", OutcomeNetworkFailure, false},
		{"bad scheme is product failure", "file:///nonexistent", "tolerant", OutcomeProductFailure, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr, cleanup := startServeServer(t)
			defer cleanup()
			runner := NewRunner(RunnerConfig{OutDir: t.TempDir(), AuthToken: smokeTestToken, StepTimeout: 5 * time.Second})
			scenario := Scenario{ID: "cause", Site: tc.url, NetworkFail: tc.mode, Steps: []Step{
				{Name: "create", Cmd: "session.new"}, {Name: "fetch", Cmd: "page.open", Params: map[string]any{"url": tc.url, "runScripts": false}},
			}}
			result := runner.runScenarioWithServeAddr(t.Context(), &scenario, addr)
			if result.Pass || result.Skipped != tc.skipped || result.Outcome != tc.want || result.HarnessError != "" {
				t.Fatalf("actual result misclassified: %+v", result)
			}
		})
	}
}

func TestSmokeIdentityCannotPassWithMissingOrUnboundedOwnership(t *testing.T) {
	cases := []struct {
		name, command string
		value         any
		invalid       bool
	}{
		{"session complete", "session.new", map[string]any{"sessionId": "s", "ownerUserRef": "u"}, false},
		{"session missing owner", "session.new", map[string]any{"sessionId": "s"}, true},
		{"session empty ID", "session.new", map[string]any{"sessionId": "", "ownerUserRef": "u"}, true},
		{"page scalar", "page.open", "p", true},
		{"page complete", "page.open", map[string]any{"pageId": "p"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if (validateSmokeIdentity(tc.command, tc.value) != nil) != tc.invalid {
				t.Fatal("identity validation differs")
			}
		})
	}
	result := ScenarioResult{Pass: true, Skipped: true, NetworkFail: true, HarnessError: "cleanup failed"}
	result.finalize(time.Second, nil)
	if result.Pass || result.Skipped || result.Outcome != OutcomeHarnessFailure || result.Duration != 1000 {
		t.Fatalf("cleanup hid behind network skip: %+v", result)
	}
	result.finalize(time.Second, context.Canceled)
	if result.Outcome != OutcomeInterrupted {
		t.Fatalf("interruption lost: %+v", result)
	}
}

func TestSmokeCLIRejectsIncompleteInvocationBeforeFilesystemEffects(t *testing.T) {
	for _, args := range [][]string{
		{"--scenarios", "missing"},
		{"--bin", "missing", "--scenarios", "missing", "--out", ""},
		{"--bin", "missing", "--scenarios", "missing", "--host", "192.0.2.1"},
		{"--bin", "missing", "--scenarios", "missing", "extra"},
	} {
		if code := Command(args); code != 2 {
			t.Fatalf("incomplete invocation exit=%d args=%v", code, args)
		}
	}
	for _, host := range []string{"0:0:0:0:0:0:0:1", "::ffff:127.0.0.1"} {
		runner := NewRunner(RunnerConfig{Host: host})
		if runner.cfg.Host != "::1" && runner.cfg.Host != "127.0.0.1" {
			t.Fatalf("numeric loopback was not normalized: %q", runner.cfg.Host)
		}
	}
}
