package main

import (
	"context"
	"flag"
	"os"

	"github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/bridge"
)

type browserPolicyFlags struct{ path, confirm *string }

func addBrowserPolicyFlags(flags *flag.FlagSet) browserPolicyFlags {
	path := os.Getenv("AGENT_BROWSER_ACTION_POLICY")
	if path == "" {
		path = os.Getenv("AGENT_BROWSER_POLICY")
	}
	return browserPolicyFlags{path: flags.String("action-policy", path, "JSON action/navigation policy file"),
		confirm: flags.String("confirm-actions", os.Getenv("AGENT_BROWSER_CONFIRM_ACTIONS"), "additional confirmation categories (comma-separated)")}
}

func (flags browserPolicyFlags) config() (artemis.AgentConfig, error) {
	confirm, err := bridge.ParseConfirmActions(*flags.confirm)
	if err != nil {
		return artemis.AgentConfig{}, err
	}
	return artemis.AgentConfig{ActionPolicyPath: *flags.path, ConfirmActions: confirm}, nil
}

func (flags browserPolicyFlags) start(ctx context.Context) (*artemis.BrowserPolicy, error) {
	config, err := flags.config()
	if err != nil {
		return nil, err
	}
	policy, err := artemis.NewBrowserPolicy(ctx, config)
	if err != nil {
		return nil, err
	}
	policy.StartHotReload()
	return policy, nil
}

func checkCLIActionPolicy(ctx context.Context, policy bridge.PolicyEngine, kind, target string) error {
	response, err := bridge.NewPolicyHook(policy).Check(ctx, bridge.PolicyRequest{Action: kind, URL: target})
	if err != nil {
		return err
	}
	if response.Decision != bridge.PolicyDecisionAllow {
		return &bridge.PolicyError{Decision: response.Decision, Reason: response.Reason}
	}
	return nil
}
