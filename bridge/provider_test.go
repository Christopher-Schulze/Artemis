package bridge

import (
	"context"
	"testing"
)

func TestProviderRegistryDefault(t *testing.T) {
	r := NewProviderRegistry()
	p := r.Default()
	if p == nil {
		t.Fatal("expected default provider")
	}
	if p.Name() != "local-chrome" {
		t.Fatalf("expected local-chrome, got %s", p.Name())
	}
}

func TestProviderRegistryGetByName(t *testing.T) {
	r := NewProviderRegistry()
	p, err := r.Get("camofox")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "camofox" {
		t.Fatalf("expected camofox, got %s", p.Name())
	}
}

func TestProviderRegistryGetUnknown(t *testing.T) {
	r := NewProviderRegistry()
	_, err := r.Get("unknown")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestProviderRegistryAvailable(t *testing.T) {
	r := NewProviderRegistry()
	names := r.Available()
	if len(names) < 2 {
		t.Fatalf("expected at least 2 providers, got %d", len(names))
	}
}

func TestLocalChromeProviderLaunch(t *testing.T) {
	p := &LocalChromeProvider{}
	t.Cleanup(func() { _ = p.Close() })
	session, err := p.Launch(context.Background(), ProviderConfig{
		SessionName: "test-session",
		Headless:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.ProviderName != "local-chrome" {
		t.Fatalf("expected local-chrome, got %s", session.ProviderName)
	}
	if session.SessionID != "test-session" {
		t.Fatalf("expected test-session, got %s", session.SessionID)
	}
	if !session.Features["headless"] {
		t.Fatal("expected headless feature")
	}
}

func TestLocalChromeProviderHealthy(t *testing.T) {
	p := &LocalChromeProvider{}
	if p.Healthy() {
		t.Fatal("provider without a validated runtime must be unhealthy")
	}
	_, err := p.Launch(context.Background(), ProviderConfig{SessionName: "health-test", Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Healthy() {
		t.Fatal("provider with a validated runtime must be healthy")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if p.Healthy() {
		t.Fatal("closed provider must be unhealthy")
	}
}

func TestLocalChromeProviderRejectsUnenforceableProxyAndExternalPolicy(t *testing.T) {
	provider := &LocalChromeProvider{}
	for _, config := range []ProviderConfig{
		{ProxyURL: "http://127.0.0.1:8080"},
		{CDPURL: "ws://127.0.0.1:9222", AllowPrivateNetworks: true},
		{CDPURL: "ws://127.0.0.1:9222", AllowedPorts: []int{8443}},
	} {
		if _, err := provider.Launch(context.Background(), config); err == nil {
			t.Fatalf("config=%+v was accepted", config)
		}
	}
}

func TestCamofoxProviderLaunchNoURL(t *testing.T) {
	p := &CamofoxProvider{}
	_, err := p.Launch(context.Background(), ProviderConfig{
		SessionName: "test",
	})
	if err == nil {
		t.Fatal("expected error when CAMOFOX_URL not set")
	}
}

func TestCamofoxProviderLaunchWithURL(t *testing.T) {
	t.Setenv("CAMOFOX_URL", "http://localhost:8888")
	p := &CamofoxProvider{}
	session, err := p.Launch(context.Background(), ProviderConfig{
		SessionName: "camofox-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.CDPURL != "http://localhost:8888" {
		t.Fatalf("expected CDP URL, got %s", session.CDPURL)
	}
	if !session.Features["fingerprint_spoofing"] {
		t.Fatal("expected fingerprint_spoofing feature")
	}
}

func TestCamofoxProviderHealthyNoURL(t *testing.T) {
	t.Setenv("CAMOFOX_URL", "")
	p := &CamofoxProvider{}
	if p.Healthy() {
		t.Fatal("camofox should not be healthy without URL")
	}
}

func TestCamofoxProviderHealthyWithURL(t *testing.T) {
	t.Setenv("CAMOFOX_URL", "http://localhost:8888")
	p := &CamofoxProvider{}
	if !p.Healthy() {
		t.Fatal("camofox should be healthy with URL")
	}
}

func TestSelectFromConfigDefault(t *testing.T) {
	r := NewProviderRegistry()
	p, _, err := r.SelectFromConfig()
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "local-chrome" {
		t.Fatalf("expected local-chrome default, got %s", p.Name())
	}
}

func TestSelectFromConfigCDPOverride(t *testing.T) {
	t.Setenv("BROWSER_CDP_URL", "ws://localhost:9222")
	r := NewProviderRegistry()
	p, config, err := r.SelectFromConfig()
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "local-chrome" {
		t.Fatalf("CDP override should force local, got %s", p.Name())
	}
	if config.CDPURL != "ws://localhost:9222" {
		t.Fatalf("expected CDP URL, got %s", config.CDPURL)
	}
}

func TestSelectFromConfigProviderEnv(t *testing.T) {
	t.Setenv("BROWSER_PROVIDER", "camofox")
	t.Setenv("CAMOFOX_URL", "http://localhost:8888")
	r := NewProviderRegistry()
	p, _, err := r.SelectFromConfig()
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "camofox" {
		t.Fatalf("expected camofox from env, got %s", p.Name())
	}
}

func TestProviderClose(t *testing.T) {
	p := &LocalChromeProvider{}
	_, err := p.Launch(context.Background(), ProviderConfig{SessionName: "test", Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}
