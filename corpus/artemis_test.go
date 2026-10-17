package corpus

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/process"
)

func TestRunArtemisAdmissionAndOwnedDirectoryCleanup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	manifests, content, err := DefaultAssets("artemis")
	if err != nil {
		t.Fatal(err)
	}
	for _, launch := range []process.LaunchConfig{
		{UserDataDir: root}, {ExtraArgs: []string{"--no-sandbox"}}, {Sandbox: process.SandboxDisabled},
		{AllowedPorts: []int{80}}, {AllowPrivateNetworks: true}, {StartupTimeout: time.Minute},
		{ResourceBudget: process.ResourceBudget{SessionTimeout: time.Minute}},
		{BinaryPath: filepath.Join(root, "missing-browser")},
	} {
		result, err := RunArtemis(t.Context(), manifests[0], content, launch)
		if err == nil || result.Passed || result.StatusCode != 0 {
			t.Fatalf("invalid launch acquired observation: %+v %v", result, err)
		}
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := RunArtemis(cancelled, manifests[0], content, process.LaunchConfig{}); !errors.Is(err, context.Canceled) || result.Passed {
		t.Fatal("cancelled runtime acquired coverage")
	}
	if result, err := RunArtemis(nil, manifests[0], content, process.LaunchConfig{}); err == nil || result.Passed {
		t.Fatal("missing owner acquired coverage")
	}
	foreign, source, err := DefaultAssets("chromium")
	if err != nil {
		t.Fatal(err)
	}
	if result, err := RunArtemis(t.Context(), foreign[0], source, process.LaunchConfig{}); err == nil || result.Passed || result.StatusCode != 0 {
		t.Fatal("Chromium manifest silently changed owner")
	}
	corrupt := append([]byte{}, content...)
	corrupt[len(corrupt)/2] ^= 1
	if result, err := RunArtemis(t.Context(), manifests[0], corrupt, process.LaunchConfig{}); err == nil || result.Passed || result.StatusCode != 0 {
		t.Fatal("changed pinned source reached observation")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed admission retained runtime files: %v %v", entries, err)
	}
}

func TestRunArtemisActualPinnedSourceAndCleanup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	manifests, data, err := DefaultAssets("artemis")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		id     string
		status int
		text   string
	}{
		{id: "html-001", status: 200, text: "HTML Fixture"},
		{id: "auth-basic-001", status: 401, text: "Unauthorized"},
	} {
		t.Run(test.id, func(t *testing.T) {
			for _, manifest := range manifests {
				if !strings.HasSuffix(manifest.ID, ":"+test.id) {
					continue
				}
				result, err := RunArtemis(t.Context(), manifest, data, process.LaunchConfig{})
				if err != nil || !result.Passed || result.StatusCode != test.status || !strings.Contains(result.Text, test.text) || result.FixtureManifest.ID != manifest.ID {
					t.Fatalf("actual runtime source/outcome: %+v %v", result, err)
				}
				entries, err := os.ReadDir(root)
				if err != nil || len(entries) != 0 {
					t.Fatalf("completed runtime retained files: %v %v", entries, err)
				}
				return
			}
			t.Fatal("required source case missing")
		})
	}
}
