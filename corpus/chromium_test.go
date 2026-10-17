package corpus

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/process"
)

func TestRunChromiumRejectsForeignOwnerAndUnscopedLaunchBeforeObservation(t *testing.T) {
	manifests, data, err := DefaultAssets("chromium")
	if err != nil {
		t.Fatal(err)
	}
	for _, launch := range []process.LaunchConfig{
		{UserDataDir: t.TempDir()}, {ExtraArgs: []string{"--no-sandbox"}}, {Sandbox: process.SandboxDisabled},
		{AllowedPorts: []int{80}}, {AllowPrivateNetworks: true}, {StartupTimeout: time.Minute},
		{ResourceBudget: process.ResourceBudget{SessionTimeout: time.Minute}},
	} {
		result, err := RunChromium(t.Context(), manifests[0], data, launch)
		if err == nil || result.Passed || result.StatusCode != 0 {
			t.Fatalf("unscoped launch acquired an observation: %+v %v", result, err)
		}
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := RunChromium(cancelled, manifests[0], data, process.LaunchConfig{}); !errors.Is(err, context.Canceled) || result.Passed || result.StatusCode != 0 {
		t.Fatal("cancelled Chromium owner acquired an observation")
	}
	renderless, source, err := DefaultAssets("renderless")
	if err != nil {
		t.Fatal(err)
	}
	if result, err := RunChromium(t.Context(), renderless[0], source, process.LaunchConfig{}); err == nil || result.Passed || result.StatusCode != 0 {
		t.Fatal("renderless manifest silently changed runner")
	}
}

func TestRunChromiumActualPinnedHTML(t *testing.T) {
	manifests, data, err := DefaultAssets("chromium")
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range manifests {
		if !strings.HasSuffix(manifest.ID, ":html-001") {
			continue
		}
		result, err := RunChromium(t.Context(), manifest, data, process.LaunchConfig{})
		if err != nil || !result.Passed || result.Title != "HTML Fixture" || result.StatusCode != 200 || result.FixtureManifest.ID != manifest.ID {
			t.Fatalf("actual owned Chromium source/outcome: %+v %v", result, err)
		}
		return
	}
	t.Fatal("actual HTML corpus case missing")
}
