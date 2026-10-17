package benchmark

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactPublicationUsesItsPinnedRootAfterDirectoryMove(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	moved := directory + "-moved"
	if _, err := os.Lstat(moved); !os.IsNotExist(err) {
		t.Fatalf("unexpected move destination: %v", err)
	}
	if err := os.Rename(directory, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("actual retained evidence bytes")
	if err := writeArtifactInRoot(root, "manifest.json", content); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(moved, "manifest.json"))
	if err != nil || !bytes.Equal(actual, content) {
		t.Fatalf("publication lost its pinned directory: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(directory, "manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("publication escaped into replacement directory: %v", err)
	}
	if err := writeArtifactInRoot(root, "manifest.json", []byte("replacement")); err == nil {
		t.Fatal("pinned publication replaced existing evidence")
	}
	actual, err = os.ReadFile(filepath.Join(moved, "manifest.json"))
	if err != nil || !bytes.Equal(actual, content) {
		t.Fatal("retained pinned evidence changed")
	}
}

func TestArtifactByteLimitsRejectBeforeWritingOrReadingExcessData(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "uncreated", "scorecard.json")
	if err := writeArtifact(path, make([]byte, MaxArtifactFileBytes+1)); err == nil {
		t.Fatal("oversized generated artifact was published")
	}
	if _, err := os.Lstat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("oversized output performed filesystem preparation: %v", err)
	}
	file, err := os.CreateTemp(parent, "sparse-large-")
	if err != nil {
		t.Fatal(err)
	}
	truncateErr := file.Truncate(MaxArtifactFileBytes + 1)
	if err := errors.Join(truncateErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	var captured bytes.Buffer
	if _, err := digestArtifact(t.Context(), root, filepath.Base(file.Name()), MaxArtifactFileBytes, &captured); err == nil || captured.Len() != 0 {
		t.Fatal("oversized sparse file was read or admitted")
	}
}

func TestArtifactStageCleanupPreservesReplacedForeignContents(t *testing.T) {
	directory := t.TempDir()
	stage := filepath.Join(directory, "stage")
	if err := os.WriteFile(stage, []byte("owned original bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	identity, err := os.Lstat(stage)
	if err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(directory, "moved")
	if _, err := os.Lstat(moved); !os.IsNotExist(err) {
		t.Fatalf("unexpected move destination: %v", err)
	}
	if err := os.Rename(stage, moved); err != nil {
		t.Fatal(err)
	}
	foreign := []byte("foreign replacement must survive")
	if err := os.WriteFile(stage, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := removeArtifactStage(root, "stage", identity); err == nil {
		t.Fatal("replaced stage was treated as owned cleanup")
	}
	actual, err := os.ReadFile(stage)
	if err != nil || !bytes.Equal(actual, foreign) {
		t.Fatalf("foreign replacement was changed: %v", err)
	}
	if err := removeArtifactStage(root, "moved", identity); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(moved); !os.IsNotExist(err) {
		t.Fatalf("actual owned stage was not cleaned: %v", err)
	}
}
