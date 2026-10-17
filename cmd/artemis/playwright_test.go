package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Christopher-Schulze/Artemis/playwright"
)

func TestPlaywrightCommandReportsAndRefusesTruthfully(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(t.TempDir(), "signer.pub")
	if err := os.WriteFile(keyFile, []byte(base64.StdEncoding.EncodeToString(public)), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	output := filepath.Join(t.TempDir(), "status.json")
	file, err := os.Create(output)
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = file
	code := cmdPlaywright([]string{"--cache", cache, "--trusted-key", keyFile})
	os.Stdout = original
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("status exit = %d", code)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var projection playwright.Projection
	if err := json.Unmarshal(raw, &projection); err != nil || projection.Availability != playwright.AvailabilityUnavailable || projection.Remediation == "" {
		t.Fatalf("status = %s %v", raw, err)
	}
	if code := cmdPlaywright([]string{"--cache", cache, "--trusted-key", keyFile, "--op", "install", "--artifact", t.TempDir()}); code != 1 {
		t.Fatalf("install of an unsigned directory exit = %d, want 1", code)
	}
	for _, args := range [][]string{{"--cache", cache}, {"--cache", cache, "--trusted-key", keyFile, "--op", "install"}, {"--cache", cache, "--trusted-key", keyFile, "--op", "delete"}} {
		if code := cmdPlaywright(args); code != 2 {
			t.Fatalf("args %v exit = %d, want 2", args, code)
		}
	}
}
