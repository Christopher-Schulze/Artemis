package policy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newFilePolicy(t *testing.T, content string) (*NavigationPolicy, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.json")
	writePolicyFile(t, path, content)
	policy, err := NewNavigationPolicy(NavigationPolicyConfig{Path: path, ReloadInterval: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(policy.Stop)
	return policy, path
}

func TestNavigationPolicyStrictCandidateAdmission(t *testing.T) {
	for _, config := range []NavigationPolicyConfig{
		{Path: "policy.json", DisabledDefault: PolicyFileAllow},
		{Path: "policy.json", DisabledDefault: PolicyFileChallenge},
		{Path: "policy.json", DisabledDefault: "unknown"},
		{Path: "policy.json", ReloadInterval: -time.Second},
		{Path: "policy.json", ReloadInterval: time.Nanosecond},
	} {
		if policy, err := NewNavigationPolicy(config); err == nil || policy != nil {
			t.Fatalf("unsafe configuration admitted: %+v", config)
		}
	}
	cases := []struct{ name, content string }{
		{"unknown_default", `{"version":1,"default":"oops"}`},
		{"empty_default", `{"version":1,"default":""}`},
		{"case_alias", `{"version":1,"default":"deny","Default":"allow"}`},
		{"case_rule_alias", `{"version":1,"rules":[{"action":"deny","Action":"allow","pattern":"https://localhost"}]}`},
		{"unknown_action", `{"version":1,"rules":[{"action":"oops","pattern":"https://localhost"}]}`},
		{"empty_pattern", `{"version":1,"rules":[{"action":"allow","pattern":""}]}`},
		{"invalid_regexp", `{"version":1,"rules":[{"action":"allow","pattern":"["}]}`},
		{"invalid_literal", `{"version":1,"rules":[{"action":"allow","pattern":"localhost"}]}`},
		{"duplicate_default", `{"version":1,"default":"deny","default":"allow"}`},
		{"duplicate_rule_action", `{"version":1,"rules":[{"action":"deny","action":"allow","pattern":"https://localhost"}]}`},
		{"unknown_field", `{"version":1,"defaut":"deny","default":"allow"}`},
		{"null_document", `null`},
		{"null_default", `{"version":1,"default":null}`},
		{"null_rules", `{"version":1,"rules":null}`},
		{"null_rule", `{"version":1,"rules":[null]}`},
		{"trailing", `{"version":1} {"version":1,"default":"allow"}`},
		{"oversized", strings.Repeat(" ", maxNavigationPolicyBytes+1)},
		{"oversized_pattern", `{"version":1,"rules":[{"action":"allow","pattern":"https://localhost/` + strings.Repeat("x", maxNavigationPolicyPatternBytes) + `"}]}`},
		{"excess_rules", `{"version":1,"rules":[` + strings.Repeat(`{"action":"allow","pattern":"https://localhost"},`, maxNavigationPolicyRules) + `{"action":"allow","pattern":"https://localhost"}]}`},
		{"invalid_utf8", "{\"version\":1,\"default\":\"\xff\"}"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			policy, _ := newFilePolicy(t, test.content)
			if policy.CurrentPolicy() != nil || policy.LastReloadError() == nil || policy.ValidateURL("https://localhost") != PolicyDecisionDeny {
				t.Fatal("invalid initial candidate published or lost its failure evidence")
			}
			for range 3 {
				if err := policy.Reload(); err == nil {
					t.Fatal("invalid reload succeeded")
				}
				mustEvaluateNavigationPolicy(t, policy, context.Background(), PolicyRequest{URL: "https://localhost"})
			}
			_, _, _, _, reloads, failures := policy.Stats()
			if reloads != 0 || failures != 1 {
				t.Fatalf("same invalid candidate inflated counters: reloads=%d errors=%d", reloads, failures)
			}
		})
	}
	policy, _ := newFilePolicy(t, `{"version":1}`)
	if policy.LastReloadError() != nil || policy.CurrentPolicy().Default != PolicyFileDeny || policy.ValidateURL("https://example.com") != PolicyDecisionDeny {
		t.Fatal("omitted default must deny")
	}
}

func TestNavigationPolicyContentReloadRetainsLastValid(t *testing.T) {
	policy, path := newFilePolicy(t, `{"version":1,"default":"deny","rules":[{"action":"allow","pattern":"https://localhost"}]}`)
	initial, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the real PolicyEngine consumer interface, not a second evaluator.
	hook := NewPolicyHook(policy)
	request := PolicyRequest{URL: "https://localhost:8443/private"}
	response, err := hook.Check(context.Background(), request)
	if err != nil || response.Decision != PolicyDecisionAllow {
		t.Fatalf("initial decision: %+v %v", response, err)
	}
	policy.StartHotReload()
	backward := initial.ModTime().Add(-time.Hour)
	replacePolicyContentAtTime(t, path, `{"version":1,"default":"deny","rules":[{"action":"challenge","pattern":"https://localhost"}]}`, backward)
	waitPolicy(t, func() bool { return policy.ValidateURL(request.URL) == PolicyDecisionChallenge })
	response, err = hook.Check(context.Background(), request)
	if err != nil || response.Decision != PolicyDecisionChallenge {
		t.Fatalf("reloaded consumer decision: %+v %v", response, err)
	}
	var workers sync.WaitGroup
	for range 32 {
		workers.Go(func() {
			policy.StartHotReload()
			if err := policy.Reload(); err != nil {
				t.Errorf("unchanged reload: %v", err)
			}
		})
	}
	workers.Wait()
	_, _, _, _, reloads, failures := policy.Stats()
	if reloads != 1 || failures != 0 {
		t.Fatalf("change counted incorrectly: reloads=%d errors=%d", reloads, failures)
	}
	replacePolicyContentAtTime(t, path, `{"version":1,"default":"wrong"}`, backward)
	waitPolicy(t, func() bool { return policy.LastReloadError() != nil })
	for range 16 {
		if err := policy.Reload(); err == nil {
			t.Fatal("invalid candidate accepted")
		}
	}
	response, err = hook.Check(context.Background(), request)
	if err != nil || response.Decision != PolicyDecisionChallenge {
		t.Fatalf("invalid reload replaced last valid decision: %+v %v", response, err)
	}
	_, _, _, _, reloads, failures = policy.Stats()
	if reloads != 1 || failures != 1 {
		t.Fatalf("invalid content inflated counts: reloads=%d errors=%d", reloads, failures)
	}
	replacePolicyContentAtTime(t, path, `{"version":1,"default":"deny","rules":[]}`, backward)
	waitPolicy(t, func() bool {
		return policy.ValidateURL(request.URL) == PolicyDecisionDeny && policy.LastReloadError() == nil
	})
	policy.Stop()
	_, _, _, _, reloads, failures = policy.Stats()
	if reloads != 2 || failures != 1 {
		t.Fatalf("valid recovery counters: reloads=%d errors=%d", reloads, failures)
	}
}

// Publish actual immutable file versions, including their selected timestamp.
// In-place writes may correctly produce a separate partial-candidate error.
func replacePolicyContentAtTime(t *testing.T, path, content string, timestamp time.Time) {
	t.Helper()
	file, err := os.CreateTemp(filepath.Dir(path), "policy-candidate-")
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString(content)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file.Name(), timestamp, timestamp); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		t.Fatal(err)
	}
}

func waitPolicy(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for !ready() {
		select {
		case <-deadline.C:
			t.Fatal("policy transition did not complete")
		case <-ticker.C:
		}
	}
}

func TestNavigationPolicyConcurrentAtomicPublicationDoesNotManufactureErrors(t *testing.T) {
	policy, path := newFilePolicy(t, `{"version":1,"default":"deny"}`)
	policy.StartHotReload()
	var callers sync.WaitGroup
	callers.Go(func() {
		for i := range 64 {
			defaultAction := "allow"
			if i%2 == 0 {
				defaultAction = "deny"
			}
			content := `{"version":1,"default":"` + defaultAction + `"}` + strings.Repeat(" ", 32<<10)
			replacePolicyContentAtTime(t, path, content, time.Unix(1, 0))
		}
	})
	for range 8 {
		callers.Go(func() {
			for range 32 {
				if err := policy.Reload(); err != nil {
					t.Errorf("atomic publication manufactured a reload error: %v", err)
				}
				response := mustEvaluateNavigationPolicy(t, policy, context.Background(), PolicyRequest{URL: "https://example.com"})
				if response.Decision != PolicyDecisionAllow && response.Decision != PolicyDecisionDeny {
					t.Errorf("invalid concurrent decision: %+v", response)
				}
			}
		})
	}
	callers.Wait()
	if err := policy.Reload(); err != nil {
		t.Fatal(err)
	}
	policy.Stop()
	_, _, _, _, _, failures := policy.Stats()
	if failures != 0 || policy.LastReloadError() != nil || policy.CurrentPolicy().Default != PolicyFileAllow {
		t.Fatalf("concurrent atomic updates corrupted policy/error state: failures=%d error=%v policy=%+v", failures, policy.LastReloadError(), policy.CurrentPolicy())
	}
}

func TestNavigationPolicyLifecycleJoinsAndIsTerminal(t *testing.T) {
	for _, stopBeforeStart := range []bool{false, true} {
		t.Run(map[bool]string{false: "started", true: "stop_before_start"}[stopBeforeStart], func(t *testing.T) {
			policy, path := newFilePolicy(t, `{"version":1,"default":"deny"}`)
			if stopBeforeStart {
				policy.Stop()
			}
			var callers sync.WaitGroup
			for range 32 {
				callers.Go(policy.StartHotReload)
			}
			callers.Wait()
			for range 32 {
				callers.Go(policy.Stop)
			}
			callers.Wait()
			if stopBeforeStart && policy.started {
				t.Fatal("stop-before-start created a poller")
			}
			if !stopBeforeStart {
				select {
				case <-policy.doneCh:
				default:
					t.Fatal("Stop returned before its poller finished")
				}
			}
			writePolicyFile(t, path, `{"version":1,"default":"allow"}`)
			policy.StartHotReload()
			if err := policy.Reload(); !errors.Is(err, ErrNavigationPolicyStopped) {
				t.Fatalf("post-stop reload: %v", err)
			}
			if policy.CurrentPolicy().Default != PolicyFileDeny || policy.ValidateURL("https://example.com") != PolicyDecisionDeny {
				t.Fatal("stopped policy changed or authorized")
			}
			response, err := policy.Evaluate(context.Background(), PolicyRequest{URL: "https://example.com"})
			if !errors.Is(err, ErrNavigationPolicyStopped) || response.Decision != PolicyDecisionDeny {
				t.Fatalf("post-stop evaluation: %+v %v", response, err)
			}
		})
	}
	policy, _ := newFilePolicy(t, `{"version":1,"default":"deny"}`)
	var callers sync.WaitGroup
	for range 64 {
		callers.Go(func() { policy.StartHotReload(); policy.Stop() })
	}
	callers.Wait()
	if !policy.stopped.Load() {
		t.Fatal("concurrent lifecycle did not stop")
	}
	if policy.started {
		select {
		case <-policy.doneCh:
		default:
			t.Fatal("concurrent Stop did not join")
		}
	}
}

func TestNavigationPolicyImmutableRulesAndHostBoundaries(t *testing.T) {
	policy, _ := newFilePolicy(t, `{"version":1,"rules":[{"action":"allow","pattern":"https://localhost/private"},{"action":"challenge","pattern":"https://safe\\.example\\.com/.*"}]}`)
	copy := policy.CurrentPolicy()
	copy.Default = PolicyFileAllow
	copy.Rules[0].Action = PolicyFileDeny
	copy.Rules[0].Pattern = "https://evil.example"
	cases := []struct {
		url  string
		want PolicyDecision
	}{
		{"https://localhost/private/a", PolicyDecisionAllow},
		{"https://localhost.evil/private", PolicyDecisionDeny},
		{"https://localhost@evil.example/private", PolicyDecisionDeny},
		{"https://evil.example/?next=https://safe.example.com/path", PolicyDecisionDeny},
		{"https://safe.example.com/path", PolicyDecisionChallenge},
		{"https://safexexamplexcom/path", PolicyDecisionDeny},
		{"https://localhost/public", PolicyDecisionDeny},
	}
	for _, test := range cases {
		if got := policy.ValidateURL(test.url); got != test.want {
			t.Errorf("%s = %s, want %s", test.url, got, test.want)
		}
		response := mustEvaluateNavigationPolicy(t, policy, context.Background(), PolicyRequest{URL: test.url})
		if response.Decision != test.want {
			t.Errorf("Evaluate %s = %s", test.url, response.Decision)
		}
	}
	allowPolicy, _ := newFilePolicy(t, `{"version":1,"default":"allow"}`)
	for _, target := range []string{"", "example.com", "https:///path", "javascript:alert(1)", "file:///tmp/a", "https://user:pass@example.com", "https://example.com:65536/", "https://example.com/ x", "https://example.com\\evil", "https://[fe80::1%25zone]/"} {
		if allowPolicy.ValidateURL(target) != PolicyDecisionDeny {
			t.Errorf("invalid target authorized: %q", target)
		}
		if _, err := ParsePolicyURL(target); err == nil {
			t.Errorf("invalid target parsed: %q", target)
		}
	}
	//lint:ignore SA1012 Intentional invalid caller input proves fail-closed handling.
	if response, err := allowPolicy.Evaluate(nil, PolicyRequest{}); err == nil || response.Decision != PolicyDecisionDeny {
		t.Fatal("nil context was admitted")
	}
	for _, target := range []string{"https://example.com/?q=yes", "https://[::1]:443/a"} {
		literal, err := decodeNavigationPolicy([]byte(`{"version":1,"rules":[{"action":"allow","pattern":"` + target + `"}]}`))
		if err != nil || !literal.Rules[0].matchRule(target) {
			t.Fatalf("literal URL rejected: %s %v", target, err)
		}
	}
}

func TestNavigationPolicyPreservesBootstrapAndRejectsNonRegular(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := WriteDefaultPolicyFile(path); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteDefaultPolicyFile(path); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing bootstrap was overwritten: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != string(original) {
		t.Fatal("existing policy bytes lost")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("bootstrap permissions: %v %v", info, err)
	}
	link := path + ".link"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{link, filepath.Dir(path)} {
		policy, err := NewNavigationPolicy(NavigationPolicyConfig{Path: source})
		if err != nil {
			t.Fatal(err)
		}
		if policy.LastReloadError() == nil || policy.CurrentPolicy() != nil {
			t.Fatal("non-regular policy admitted")
		}
		policy.Stop()
	}
}
