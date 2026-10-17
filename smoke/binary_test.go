package smoke

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSmokeBinarySnapshotAndMutationAdmission(t *testing.T) {
	binary := buildRealSmokeArtemis(t)
	runner := prepareEvidenceRunner(t, binary, t.TempDir())
	ownedDirectory := runner.binaryDir
	original := runner.binary
	file, err := os.OpenFile(binary, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.Write([]byte("test-owned source changed after immutable snapshot"))
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	if err := runner.verifyBinary(); err != nil {
		t.Fatalf("caller mutation changed owned snapshot: %v", err)
	}
	actual, _, err := hashSmokeBinary(binary)
	if err != nil || actual == original.SHA256 {
		t.Fatalf("test failed to change actual source bytes: %v", err)
	}
	sf, _ := loadSmokeLifecycle(t)
	sc, err := runner.RunAll(t.Context(), sf)
	if err != nil || sc.Binary != original || sc.Passed != 1 {
		t.Fatalf("frozen actual binary not tested: %+v %v", sc, err)
	}
	if _, err := os.Stat(ownedDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary binary retained: %v", err)
	}
	if digest, _, err := hashSmokeBinary(binary); err != nil || digest != actual {
		t.Fatalf("cleanup altered caller binary: %v", err)
	}

	runner = prepareEvidenceRunner(t, binary, t.TempDir())
	if err := os.Chmod(runner.binPath, 0o700); err != nil {
		t.Fatal(err)
	}
	file, err = os.OpenFile(runner.binPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr = file.Write([]byte("owned binary corruption"))
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	if owner, client, err := runner.startServe(t.Context()); err == nil || owner != nil || client != nil || len(runner.children) != 0 {
		t.Fatalf("mutated owned binary spawned: %v", err)
	}
}

func TestSmokeBinaryRejectedInputsLeaveNoOwnedDirectory(t *testing.T) {
	root := t.TempDir()
	nonExecutable := filepath.Join(root, "not-executable")
	if err := os.WriteFile(nonExecutable, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	invalid := filepath.Join(root, "not-go-binary")
	if err := os.WriteFile(invalid, []byte("invalid"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "missing"), root, nonExecutable, invalid} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			runner := NewRunner(RunnerConfig{ArtemisBinPath: path})
			if err := runner.BuildArtemis(t.Context()); err == nil {
				t.Fatal("invalid binary admitted")
			}
			if runner.binaryDir != "" || runner.binPath != "" || len(runner.children) != 0 {
				t.Fatalf("rejected preparation leaked owner: %+v", runner)
			}
		})
	}
}

func TestSmokeBinaryUnavailableVCSCannotClaimKnownCleanBuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artemis")
	command := exec.CommandContext(t.Context(), "go", "build", "-buildvcs=false", "-o", path, "../cmd/artemis")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build actual binary without VCS metadata: %v %s", err, output)
	}
	runner := prepareEvidenceRunner(t, path, t.TempDir())
	if runner.binary.CommitKnown || runner.binary.Commit != "" || runner.binary.ModifiedKnown || runner.binary.Modified || runner.binary.SHA256 == "" || runner.binary.Version == "" {
		t.Fatalf("unavailable VCS became fabricated clean identity: %+v", runner.binary)
	}
}
