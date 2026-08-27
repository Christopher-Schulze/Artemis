package diagnostics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStorePersistsClosedRedactedSchema(t *testing.T) {
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "audit", "artemis.jsonl")
	store, err := NewStore(Config{Path: path, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	decision := PolicyDecision{
		Operation: "navigation", Transport: "https", Host: "example.test", Port: 443,
		Result: "allow", ReasonCode: "policy_match", SessionRef: HashSession("raw-session"),
	}
	if appendPolicyErr := store.AppendPolicy(decision); appendPolicyErr != nil {
		t.Fatal(appendPolicyErr)
	}
	if appendResourceErr := store.AppendResource(ResourceUsage{Scope: "renderless", SessionRef: HashSession("raw-session"), Requests: 2, ResponseBytes: 64, DiskBytes: 32}); appendResourceErr != nil {
		t.Fatal(appendResourceErr)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"raw-session", "/private", "token=", "cookie", "password", "page_content"} {
		if strings.Contains(strings.ToLower(string(data)), forbidden) {
			t.Fatalf("ledger leaked %q: %s", forbidden, data)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("ledger mode=%o, want 600", info.Mode().Perm())
	}
	records, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Policy == nil || records[1].Resource == nil {
		t.Fatalf("records=%+v", records)
	}
}

func TestStoreEnforcesAgeCountAndByteRetention(t *testing.T) {
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "artemis.jsonl")
	store, err := NewStore(Config{
		Path: path, MaxRecords: 2, MaxBytes: 520, MaxAge: time.Hour,
		MaxRecordBytes: 256, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 4; index++ {
		if appendErr := store.AppendPolicy(PolicyDecision{
			Operation: "navigation", Transport: "https", Host: "example.test", Port: 443,
			Result: "deny", ReasonCode: "host_not_allowed", SessionRef: HashSession(string(rune('a' + index))),
		}); appendErr != nil {
			t.Fatal(appendErr)
		}
	}
	records, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("count retention kept %d records, want 2", len(records))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 520 {
		t.Fatalf("byte retention size=%d, want <=520", info.Size())
	}
	now = now.Add(2 * time.Hour)
	records, err = store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("age retention kept %d expired records", len(records))
	}
}

func TestStoreEnforcesByteRetentionIndependently(t *testing.T) {
	store, err := NewStore(Config{MaxRecords: 10, MaxBytes: 500, MaxRecordBytes: 300})
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"one.example", "two.example", "three.example"} {
		if appendErr := store.AppendPolicy(PolicyDecision{
			Operation: "navigation", Transport: "https", Host: host, Port: 443,
			Result: "allow", ReasonCode: "policy_match", SessionRef: HashSession(host),
		}); appendErr != nil {
			t.Fatal(appendErr)
		}
	}
	records, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, record := range records {
		total += recordSize(record)
	}
	if len(records) == 0 || len(records) >= 3 || total > 500 || records[len(records)-1].Policy == nil || records[len(records)-1].Policy.Host != "three.example" {
		t.Fatalf("records=%+v bytes=%d", records, total)
	}
}

func TestStoreRejectsSensitiveOrCorruptRecords(t *testing.T) {
	store, err := NewStore(Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, decision := range []PolicyDecision{
		{Operation: "navigation", Transport: "https", Host: "example.test/path?token=secret", Port: 443, Result: "allow", ReasonCode: "policy_match"},
		{Operation: "navigation", Transport: "https", Host: "example.test", Port: 443, Result: "allow", ReasonCode: "raw reason with spaces"},
		{Operation: "navigation", Transport: "https", Host: "example.test", Port: 443, Result: "allow", ReasonCode: "policy_match", SessionRef: "raw-session"},
	} {
		if appendErr := store.AppendPolicy(decision); appendErr == nil {
			t.Fatalf("accepted unsafe decision %+v", decision)
		}
	}
	path := filepath.Join(t.TempDir(), "artemis.jsonl")
	if err := os.WriteFile(path, []byte(`{"timestamp":"2026-07-18T10:00:00Z","type":"policy_decision","policy":{"operation":"navigation","transport":"https","result":"allow","reason_code":"policy_match"},"page_content":"secret"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(Config{Path: path}); err == nil {
		t.Fatal("accepted unknown sensitive field")
	}
}

func TestStoreRefreshesConcurrentWriterWithoutLosingRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artemis.jsonl")
	first, err := NewStore(Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewStore(Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	appendDecision := func(store *Store, host string) {
		t.Helper()
		if appendErr := store.AppendPolicy(PolicyDecision{Operation: "navigation", Transport: "https", Host: host, Port: 443, Result: "allow", ReasonCode: "policy_match"}); appendErr != nil {
			t.Fatal(appendErr)
		}
	}
	appendDecision(first, "one.example")
	appendDecision(second, "two.example")
	records, err := first.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Policy.Host != "one.example" || records[1].Policy.Host != "two.example" {
		encoded, _ := json.Marshal(records)
		t.Fatalf("concurrent records=%s", encoded)
	}
}

func TestStoreRefreshesSameSizeExternalReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artemis.jsonl")
	store, err := NewStore(Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if appendErr := store.AppendPolicy(PolicyDecision{Operation: "navigation", Transport: "https", Host: "one.example", Port: 443, Result: "allow", ReasonCode: "policy_match"}); appendErr != nil {
		t.Fatal(appendErr)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := []byte(strings.Replace(string(original), "one.example", "two.example", 1))
	if len(replacement) != len(original) {
		t.Fatalf("replacement size=%d, want %d", len(replacement), len(original))
	}
	if writeErr := os.WriteFile(path, replacement, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	changed := time.Now().Add(time.Second)
	if chtimesErr := os.Chtimes(path, changed, changed); chtimesErr != nil {
		t.Fatal(chtimesErr)
	}
	records, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Policy == nil || records[0].Policy.Host != "two.example" {
		t.Fatalf("records=%+v", records)
	}
}
