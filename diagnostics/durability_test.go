package diagnostics

import (
	"os"
	"path/filepath"
	"testing"
)

func policyRecord(host string) PolicyDecision {
	return PolicyDecision{Operation: "navigation", Transport: "https", Host: host, Port: 443, Result: "allow", ReasonCode: "policy_match"}
}

func TestTornTailIsDroppedAndAcknowledgedRecordsSurvive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit", "artemis.jsonl")
	store, err := NewStore(Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendPolicy(policyRecord("one.example")); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte(`{"timestamp":"2026-07-18T10:00:00Z","type":"policy_dec`)); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(Config{Path: path})
	if err != nil {
		t.Fatalf("a crash-torn tail poisoned the store: %v", err)
	}
	if err := reopened.AppendPolicy(policyRecord("two.example")); err != nil {
		t.Fatal(err)
	}
	records, err := reopened.Snapshot()
	if err != nil || len(records) != 2 || records[0].Policy.Host != "one.example" || records[1].Policy.Host != "two.example" {
		t.Fatalf("acknowledged records lost or torn tail kept: %+v %v", records, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || raw[len(raw)-1] != '\n' {
		t.Fatalf("ledger not rewritten to complete records: %q %v", raw, err)
	}
}

func TestSymlinkedLedgerOrDirectoryIsRefused(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "elsewhere.jsonl")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "audit")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "artemis.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(Config{Path: filepath.Join(dir, "artemis.jsonl")}); err == nil {
		t.Fatal("symlinked ledger accepted")
	}
	linkedDir := filepath.Join(root, "linked")
	if err := os.Symlink(t.TempDir(), linkedDir); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(Config{Path: filepath.Join(linkedDir, "artemis.jsonl")}); err == nil {
		t.Fatal("symlinked ledger directory accepted")
	}
	looseDir := filepath.Join(root, "loose")
	if err := os.Mkdir(looseDir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(looseDir, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(Config{Path: filepath.Join(looseDir, "artemis.jsonl")}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(looseDir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("ledger directory not made private: %v %v", info.Mode(), err)
	}
}

func TestReplacementWithSameSizeAndMtimeIsDetected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artemis.jsonl")
	store, err := NewStore(Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendPolicy(policyRecord("aaaa.example")); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := []byte(string(original[:len(original)-len("aaaa.example\",\"port\":443,\"result\":\"allow\",\"reason_code\":\"policy_match\"}}\n")]) + "bbbb.example\",\"port\":443,\"result\":\"allow\",\"reason_code\":\"policy_match\"}}\n")
	if len(replacement) != len(original) {
		t.Fatalf("fixture lengths differ: %d %d", len(replacement), len(original))
	}
	staged := path + ".staged"
	if err := os.WriteFile(staged, replacement, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(staged, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(staged, path); err != nil {
		t.Fatal(err)
	}
	records, err := store.Snapshot()
	if err != nil || len(records) != 1 || records[0].Policy.Host != "bbbb.example" {
		t.Fatalf("same-size same-mtime replacement went unnoticed: %+v %v", records, err)
	}
}

func TestSessionRefsAreInstallationScoped(t *testing.T) {
	first := filepath.Join(t.TempDir(), "artemis.jsonl")
	a, err := NewStore(Config{Path: first})
	if err != nil {
		t.Fatal(err)
	}
	again, err := NewStore(Config{Path: first})
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewStore(Config{Path: filepath.Join(t.TempDir(), "artemis.jsonl")})
	if err != nil {
		t.Fatal(err)
	}
	memory, err := NewStore(Config{})
	if err != nil {
		t.Fatal(err)
	}
	ref := a.SessionRef("session-1")
	if ref == "" || ref != again.SessionRef("session-1") || ref == other.SessionRef("session-1") || ref == memory.SessionRef("session-1") || ref == a.SessionRef("session-2") {
		t.Fatalf("session refs are not stable per installation and unlinkable across installations: %s", ref)
	}
	if err := validateSessionRef(ref); err != nil {
		t.Fatal(err)
	}
	keyInfo, err := os.Stat(filepath.Join(filepath.Dir(first), "session.key"))
	if err != nil || keyInfo.Mode().Perm() != 0o600 || keyInfo.Size() != 32 {
		t.Fatalf("session key not private: %v %v", keyInfo, err)
	}
}
