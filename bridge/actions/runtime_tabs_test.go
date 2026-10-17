package actions

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/bridge/tabs"
)

type partialCreateTargetSource struct {
	target      tabs.Target
	listTargets []tabs.Target
	createErr   error
	closeErr    error
}

func (s *partialCreateTargetSource) ListTargets(context.Context) ([]tabs.Target, error) {
	targets := append([]tabs.Target(nil), s.listTargets...)
	if s.target.ID != "" {
		targets = append(targets, s.target)
	}
	return targets, nil
}

func (s *partialCreateTargetSource) CreateTarget(context.Context, string) (tabs.Target, error) {
	return s.target, s.createErr
}

func (s *partialCreateTargetSource) ActivateTarget(context.Context, string) error { return nil }

func (s *partialCreateTargetSource) CloseTarget(context.Context, string) error { return s.closeErr }

func TestTASK2784_TabOpenAndListUseRuntimeOwner(t *testing.T) {
	source := &partialCreateTargetSource{
		target: tabs.Target{ID: "owned-target", State: "attached"},
		listTargets: []tabs.Target{
			{ID: "root-target", UserID: "session-owner", State: "active"},
			{ID: "foreign-target", UserID: "other-owner", State: "attached"},
		},
	}
	registry := tabs.NewTabRegistry(source)
	runtime := &Runtime{tabs: registry, userID: "session-owner"}

	opened := runtime.tabOpen(context.Background(), Request{Kind: KindTabOpen, URL: "about:blank"}, Evidence{Action: KindTabOpen})
	if !opened.Success || opened.TargetID != "owned-target" {
		t.Fatalf("tab open outcome=%#v", opened)
	}
	created, ok := registry.GetTab(opened.TargetID)
	if !ok || created.UserID != "session-owner" {
		t.Fatalf("tab owner=%#v found=%v, want session-owner", created, ok)
	}

	listed := runtime.tabList(context.Background(), Evidence{Action: KindTabList})
	ids, ok := listed.Value.([]string)
	if !listed.Success || !ok || len(ids) != 2 || ids[0] != opened.TargetID || ids[1] != "root-target" {
		t.Fatalf("owned tab listing=%#v, want the session root and created target only", listed)
	}
	if _, found := registry.GetTab("foreign-target"); !found {
		t.Fatal("registry should retain the foreign target while excluding it from this owner's listing")
	}
}

func TestTASK2784_TabOpenReturnsIdentityWhenCompensationFails(t *testing.T) {
	source := &partialCreateTargetSource{
		target:    tabs.Target{ID: "uncertain-target", State: "attached"},
		createErr: &tabs.TargetCreateError{TargetID: "uncertain-target", Err: errors.New("target metadata unavailable")},
		closeErr:  errors.New("target close unavailable"),
	}
	runtime := &Runtime{tabs: tabs.NewTabRegistry(source)}
	evidence := Evidence{Action: KindTabOpen, Effect: EffectPossible}

	outcome := runtime.tabOpen(context.Background(), Request{Kind: KindTabOpen}, evidence)
	if outcome.Success || outcome.TargetID != "uncertain-target" || outcome.Failure != FailureProtocol {
		t.Fatalf("partial creation outcome=%#v", outcome)
	}
	if outcome.Evidence.Effect != EffectPossible {
		t.Fatalf("partial creation lost effect uncertainty: %#v", outcome.Evidence)
	}
}

func TestTASK2784_TabOpenReportsUnknownCreateEffectWithoutIdentity(t *testing.T) {
	source := &partialCreateTargetSource{
		createErr: &tabs.TargetCreateError{OutcomeUnknown: true, Err: errors.New("create reply was lost")},
	}
	runtime := &Runtime{tabs: tabs.NewTabRegistry(source)}
	outcome := runtime.tabOpen(context.Background(), Request{Kind: KindTabOpen}, Evidence{Action: KindTabOpen, Effect: EffectPossible})
	if outcome.Success || outcome.TargetID != "" || !outcome.OutcomeUnknown || outcome.Failure != FailureProtocol || outcome.Evidence.Effect != EffectPossible {
		t.Fatalf("unknown creation outcome=%#v", outcome)
	}
	if !strings.Contains(outcome.Error, "outcome unknown") {
		t.Fatalf("creation uncertainty was not explicit: %q", outcome.Error)
	}
	raw, err := json.Marshal(outcome)
	if err != nil {
		t.Fatal(err)
	}
	var serialized struct {
		OutcomeUnknown bool `json:"outcomeUnknown"`
	}
	if err := json.Unmarshal(raw, &serialized); err != nil || !serialized.OutcomeUnknown {
		t.Fatalf("serialized creation outcome=%s error=%v, want machine-readable unknown effect", raw, err)
	}
	if _, found := runtime.tabs.GetTab(""); found {
		t.Fatal("unknown target identity was inserted into the registry")
	}
}

func TestTASK2784_PageTargetSourcePreservesUnknownCreateOutcome(t *testing.T) {
	target, err := targetCreateFailure(&bridge.TargetError{
		State: bridge.TargetStateDetached, Message: "create receipt lost", OutcomeUnknown: true,
	})
	var createErr *tabs.TargetCreateError
	if !errors.As(err, &createErr) || !createErr.OutcomeUnknown || target.ID != "" || createErr.TargetID != "" {
		t.Fatalf("target=%#v error=%#v", target, err)
	}
}
