package main

import (
	"testing"

	"github.com/Christopher-Schulze/Artemis/smoke"
)

// The entry point delegates to smoke.Command; its contract is that an
// incomplete or malformed invocation exits with the usage code 2 before any
// runner, build or browser work starts.
func TestSmokeEntryRejectsIncompleteInvocations(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"no flags", nil},
		{"scenarios without binary or build dir", []string{"--scenarios", "scenarios.yaml"}},
		{"unknown flag", []string{"--does-not-exist"}},
		{"positional argument", []string{"--scenarios", "s.yaml", "--bin", "artemis", "stray"}},
		{"invalid host", []string{"--scenarios", "s.yaml", "--bin", "artemis", "--host", "not-an-ip"}},
		{"help", []string{"-h"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code := smoke.Command(tc.args); code != 2 {
				t.Fatalf("args %v: want usage exit code 2, got %d", tc.args, code)
			}
		})
	}
}
