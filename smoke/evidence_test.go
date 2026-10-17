package smoke

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func loadSmokeLifecycle(t *testing.T) (*ScenarioFile, []byte) {
	t.Helper()
	raw := []byte("version: proof-1\nscenarios:\n  - id: lifecycle\n    site: https://example.test\n    steps:\n      - name: create\n        cmd: session.new\n      - name: close\n        cmd: session.close\n")
	path := filepath.Join(t.TempDir(), "scenarios.yaml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	sf, err := LoadScenarios(path)
	if err != nil {
		t.Fatal(err)
	}
	return sf, raw
}

func prepareEvidenceRunner(t *testing.T, binary, parent string) *Runner {
	t.Helper()
	runner := NewRunner(RunnerConfig{ArtemisBinPath: binary, OutDir: parent, StepTimeout: 5 * time.Second})
	t.Cleanup(func() {
		if err := errors.Join(runner.Close(), runner.cleanupEvidence()); err != nil {
			t.Error(err)
		}
	})
	if err := runner.BuildArtemis(t.Context()); err != nil {
		t.Fatal(err)
	}
	return runner
}

func TestSmokeEvidenceRealRunAll(t *testing.T) {
	binary := buildRealSmokeArtemis(t)
	sf, raw := loadSmokeLifecycle(t)
	beforeMatrix, err := sf.matrixDigest()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("four concurrent completed runs", func(t *testing.T) {
		parent := t.TempDir()
		type completed struct {
			sc  *Scorecard
			err error
		}
		results := make(chan completed, 4)
		for range cap(results) {
			runner := prepareEvidenceRunner(t, binary, parent)
			directory := runner.binaryDir
			go func() {
				sc, err := runner.RunAll(t.Context(), sf)
				if _, statErr := os.Stat(directory); !errors.Is(statErr, os.ErrNotExist) {
					err = errors.Join(err, errors.New("owned binary directory remains after RunAll"), statErr)
				}
				results <- completed{sc, err}
			}()
		}
		runs, addresses, instances, pids := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[int]bool{}
		digest := sha256.Sum256(raw)
		for range cap(results) {
			var result completed
			select {
			case result = <-results:
			case <-time.After(30 * time.Second):
				t.Fatal("concurrent RunAll did not join")
			}
			sc := result.sc
			if result.err != nil || sc == nil {
				t.Fatalf("RunAll: %v %+v", result.err, sc)
			}
			if sc.SchemaVersion != 2 || sc.Passed != 1 || sc.Failed != 0 || sc.Skipped != 0 || sc.HarnessFailed != 0 || !sc.FinishedAt.After(sc.StartedAt) {
				t.Fatalf("untruthful completion: %+v", sc)
			}
			if sc.Binary.SHA256 == "" || sc.Binary.Bytes <= 0 || sc.Binary.Version == "" || sc.Binary.GoVersion == "" || sc.Binary.CommitKnown != (sc.Binary.Commit != "") {
				t.Fatalf("binary provenance missing: %+v", sc.Binary)
			}
			actualHash, actualSize, err := hashSmokeBinary(binary)
			if err != nil || actualHash != sc.Binary.SHA256 || actualSize != sc.Binary.Bytes {
				t.Fatalf("binary provenance differs from actual bytes: %v", err)
			}
			if sc.Inputs.ScenarioSHA256 != hex.EncodeToString(digest[:]) || sc.Inputs.MatrixSHA256 != beforeMatrix || sc.Inputs.ConfigSHA256 == "" {
				t.Fatalf("scenario/config provenance missing: %+v", sc.Inputs)
			}
			owner := sc.Results[0].ServeOwner
			if owner == nil || runs[sc.RunID] || addresses[owner.Address] || instances[owner.InstanceID] || pids[owner.ProcessID] {
				t.Fatalf("runs shared identity: %+v", sc)
			}
			runs[sc.RunID], addresses[owner.Address], instances[owner.InstanceID], pids[owner.ProcessID] = true, true, true, true
			process, err := os.FindProcess(owner.ProcessID)
			if err != nil {
				t.Fatal(err)
			}
			if err := process.Signal(syscall.Signal(0)); err == nil {
				t.Fatal("published scorecard has a live owned process")
			}
			for _, artifact := range sc.Results[0].Artifacts {
				data, err := os.ReadFile(filepath.Join(sc.OutputDir, artifact.Path))
				if err != nil {
					t.Fatal(err)
				}
				digest := sha256.Sum256(data)
				if int64(len(data)) != artifact.Bytes || hex.EncodeToString(digest[:]) != artifact.SHA256 {
					t.Fatal("published artifact bytes differ from scorecard")
				}
			}
			data, err := os.ReadFile(filepath.Join(sc.OutputDir, "scorecard.json"))
			if err != nil {
				t.Fatal(err)
			}
			var persisted Scorecard
			if err := json.Unmarshal(data, &persisted); err != nil || persisted.RunID != sc.RunID || persisted.Binary.SHA256 != sc.Binary.SHA256 {
				t.Fatalf("published scorecard differs: %v", err)
			}
		}
		entries, err := os.ReadDir(parent)
		if err != nil || len(entries) != 4 {
			t.Fatalf("partial staging/reservations remain: %v %+v", err, entries)
		}
		for _, entry := range entries {
			if !runs[entry.Name()] {
				t.Fatalf("unexpected output owner %s", entry.Name())
			}
		}
	})
	t.Run("reject mutated loaded matrix", func(t *testing.T) {
		matrix, _ := loadSmokeLifecycle(t)
		matrix.Scenarios[0].Steps[0].Name = "mutated"
		parent := t.TempDir()
		runner := prepareEvidenceRunner(t, binary, parent)
		if _, err := runner.RunAll(t.Context(), matrix); err == nil {
			t.Fatal("mutated matrix became provenance-bound evidence")
		}
		entries, err := os.ReadDir(parent)
		if err != nil || len(entries) != 0 || len(runner.children) != 0 {
			t.Fatalf("rejected matrix produced effects: %v %+v", err, entries)
		}
	})
	t.Run("interrupted run cannot publish", func(t *testing.T) {
		parent := t.TempDir()
		runner := prepareEvidenceRunner(t, binary, parent)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := runner.RunAll(ctx, sf); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation disappeared: %v", err)
		}
		if err := runner.Close(); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(parent)
		if err != nil || len(entries) != 0 {
			t.Fatalf("interruption retained partial evidence: %v %+v", err, entries)
		}
	})
	t.Run("output failure forces CLI failure", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "output-file")
		if err := os.WriteFile(path, []byte("preserve"), 0o600); err != nil {
			t.Fatal(err)
		}
		matrixPath := filepath.Join(t.TempDir(), "scenarios.yaml")
		if err := os.WriteFile(matrixPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if code := Command([]string{"--bin", binary, "--out", path, "--scenarios", matrixPath}); code != 1 {
			t.Fatalf("evidence failure exit=%d", code)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "preserve" {
			t.Fatalf("foreign output altered: %q %v", data, err)
		}
	})
	t.Run("completed product failure stays nonzero", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "scenario.yaml")
		raw := []byte("version: proof-1\nscenarios:\n  - id: bad-url\n    site: file:///nonexistent\n    steps:\n      - name: create\n        cmd: session.new\n      - name: fetch\n        cmd: page.open\n        params:\n          url: file:///nonexistent\n          runScripts: false\n")
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		matrix, err := LoadScenarios(path)
		if err != nil {
			t.Fatal(err)
		}
		runner := prepareEvidenceRunner(t, binary, t.TempDir())
		sc, err := runner.RunAll(t.Context(), matrix)
		if err != nil || sc.Failed != 1 || sc.ProductFailed != 1 || sc.HarnessFailed != 0 || sc.NetworkFailed != 0 || sc.Skipped != 0 {
			t.Fatalf("published failure counters: %+v %v", sc, err)
		}
		if code := Command([]string{"--scenarios", path, "--bin", binary, "--out", t.TempDir()}); code != 1 {
			t.Fatalf("completed failure exit=%d", code)
		}
	})
	afterMatrix, err := sf.matrixDigest()
	if err != nil || beforeMatrix != afterMatrix {
		t.Fatalf("concurrent ID injection mutated input matrix: %v", err)
	}
}

func TestAtomicEvidenceRejectsUnpersistedCorruptAndForeignBytes(t *testing.T) {
	cases := []struct {
		name          string
		breakEvidence func(*testing.T, *Runner, *EvidenceArtifact)
	}{
		{"missing artifact", func(t *testing.T, r *Runner, artifact *EvidenceArtifact) { artifact.Path = "missing" }},
		{"wrong digest", func(t *testing.T, r *Runner, artifact *EvidenceArtifact) { artifact.SHA256 = strings.Repeat("0", 64) }},
		{"foreign publication contents", func(t *testing.T, r *Runner, artifact *EvidenceArtifact) {
			if err := os.WriteFile(filepath.Join(r.publishedDir, "foreign"), []byte("retained"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"scorecard collision", func(t *testing.T, r *Runner, artifact *EvidenceArtifact) {
			if err := os.WriteFile(filepath.Join(r.stageDir, "scorecard.json"), []byte("existing"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := NewRunner(RunnerConfig{OutDir: t.TempDir()})
			if err := runner.beginEvidence(); err != nil {
				t.Fatal(err)
			}
			stage, final := runner.stageDir, runner.publishedDir
			artifact, err := runner.persistArtifact("actual.json", []byte(`{"actual":true}`))
			if err != nil {
				t.Fatal(err)
			}
			tc.breakEvidence(t, runner, &artifact)
			sc := &Scorecard{OutputDir: filepath.Join(final, "evidence"), Results: []ScenarioResult{{Artifacts: []EvidenceArtifact{artifact}}}}
			if err := runner.writeScorecard(sc); err == nil {
				t.Fatal("broken provenance published")
			}
			if _, err := os.Stat(filepath.Join(sc.OutputDir, "scorecard.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("partial scorecard: %v", err)
			}
			cleanupErr := runner.cleanupEvidence()
			if tc.name == "foreign publication contents" {
				if cleanupErr == nil {
					t.Fatal("foreign publication contents silently discarded")
				}
				data, err := os.ReadFile(filepath.Join(final, "foreign"))
				if err != nil || string(data) != "retained" {
					t.Fatalf("foreign data lost: %v", err)
				}
			} else if cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
			if _, err := os.Stat(stage); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed staging remains: %v", err)
			}
		})
	}
}

func TestScorecardRejectsUnverifiedStepArtifactReference(t *testing.T) {
	runner := NewRunner(RunnerConfig{OutDir: t.TempDir()})
	if err := runner.beginEvidence(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := runner.cleanupEvidence(); err != nil {
			t.Error(err)
		}
	}()
	artifact, err := runner.persistArtifact("actual", []byte("evidence"))
	if err != nil {
		t.Fatal(err)
	}
	falseReference := artifact
	falseReference.SHA256 = strings.Repeat("0", 64)
	sc := &Scorecard{OutputDir: filepath.Join(runner.publishedDir, "evidence"), Results: []ScenarioResult{{Artifacts: []EvidenceArtifact{artifact}, Steps: []StepResult{{ValueArtifact: &falseReference}}}}}
	if err := runner.writeScorecard(sc); err == nil {
		t.Fatal("unverified step artifact published")
	}
}

func TestArtifactBoundsAndExclusiveWrites(t *testing.T) {
	runner := NewRunner(RunnerConfig{OutDir: t.TempDir()})
	if _, err := runner.persistArtifact("../outside", []byte("x")); err == nil {
		t.Fatal("nonlocal artifact admitted")
	}
	runner.outputBytes = smokeOutputBytesLimit
	if _, err := runner.persistArtifact("over-budget", []byte("x")); err == nil {
		t.Fatal("aggregate bound bypassed")
	}
	runner.outputBytes = 0
	if _, err := runner.persistArtifact("oversized", make([]byte, smokeArtifactBytesLimit+1)); err == nil {
		t.Fatal("entry bound bypassed")
	}
	artifact, err := runner.persistArtifact("owned", []byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.persistArtifact("owned", []byte("replacement")); err == nil {
		t.Fatal("existing artifact overwritten")
	}
	data, err := os.ReadFile(filepath.Join(runner.cfg.OutDir, artifact.Path))
	if err != nil || string(data) != "original" || runner.outputBytes != 8 {
		t.Fatalf("failed write changed bytes/budget: %q %v %d", data, err, runner.outputBytes)
	}
}

func TestChangedTemporaryDirectoryIdentityIsRetained(t *testing.T) {
	for _, owner := range []string{"binary", "staging"} {
		t.Run(owner, func(t *testing.T) {
			runner := NewRunner(RunnerConfig{OutDir: t.TempDir()})
			var path string
			if owner == "staging" {
				if err := runner.beginEvidence(); err != nil {
					t.Fatal(err)
				}
				path = runner.stageDir
			} else {
				path = filepath.Join(t.TempDir(), "binary-owner")
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				info, err := os.Lstat(path)
				if err != nil {
					t.Fatal(err)
				}
				runner.binaryDir, runner.binaryInfo = path, info
			}
			moved := path + "-original"
			if err := os.Rename(path, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(path, "foreign"), []byte("retained"), 0o600); err != nil {
				t.Fatal(err)
			}
			var cleanupErr error
			if owner == "staging" {
				cleanupErr = runner.cleanupEvidence()
			} else {
				cleanupErr = runner.Close()
			}
			if cleanupErr == nil {
				t.Fatal("foreign directory replacement silently cleaned")
			}
			data, err := os.ReadFile(filepath.Join(path, "foreign"))
			if err != nil || string(data) != "retained" {
				t.Fatalf("replacement data lost: %v", err)
			}
			if _, err := os.Stat(moved); err != nil {
				t.Fatalf("original owner lost: %v", err)
			}
		})
	}
}
