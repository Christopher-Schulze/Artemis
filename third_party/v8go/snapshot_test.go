// Copyright 2026 the artemis authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package v8go

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSnapshotBlobIsValidRejectsEmptyAndMalformedData(t *testing.T) {
	if SnapshotBlobIsValid(nil) {
		t.Fatal("SnapshotBlobIsValid accepted an empty blob")
	}
	if SnapshotBlobIsValid(make([]byte, 128)) {
		t.Fatal("SnapshotBlobIsValid accepted malformed data")
	}
}

const processSnapshotChild = "V8GO_PROCESS_SNAPSHOT_CHILD"

// TestProcessSnapshotSharedByEveryIsolate builds a snapshot here and proves,
// in a fresh process, that every isolate starts from it: the scripted realm
// carries the baked state, plain contexts stay pristine, and no second
// snapshot can be mixed in.
func TestProcessSnapshotSharedByEveryIsolate(t *testing.T) {
	if path := os.Getenv(processSnapshotChild); path != "" {
		assertProcessSnapshotChild(t, path)
		return
	}
	creator := NewSnapshotCreator()
	if err := creator.RunScript(`globalThis.baked = {answer: 42}; Object.defineProperty(globalThis, "label", {value: "artemis"});`, "baked.js"); err != nil {
		t.Fatal(err)
	}
	blob, err := creator.CreateBlob()
	creator.Dispose()
	if err != nil {
		t.Fatal(err)
	}
	if !SnapshotBlobIsValid(blob) {
		t.Fatal("created snapshot is not valid")
	}
	if err := SetProcessSnapshot(blob); !errors.Is(err, ErrV8Initialized) {
		t.Fatalf("process snapshot accepted after V8 initialized: %v", err)
	}
	path := filepath.Join(t.TempDir(), "snapshot.bin")
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessSnapshotSharedByEveryIsolate$", "-test.count=1")
	cmd.Env = append(os.Environ(), processSnapshotChild+"="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("process snapshot child failed: %v\n%s", err, out)
	}
}

func assertProcessSnapshotChild(t *testing.T, path string) {
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetProcessSnapshot(blob); err != nil {
		t.Fatal(err)
	}
	if err := SetProcessSnapshot(blob); err != nil {
		t.Fatalf("the same snapshot was refused: %v", err)
	}
	if err := SetProcessSnapshot(append([]byte{0}, blob...)); err == nil {
		t.Fatal("a second, different snapshot was accepted")
	}
	if creator := NewSnapshotCreator(); creator.RunScript("1", "x") == nil {
		t.Fatal("a snapshot creator ran inside a process snapshot")
	}
	for round := 0; round < 2; round++ {
		iso := NewIsolate()
		scripted, err := NewContextFromSnapshot(iso, 0)
		if err != nil {
			t.Fatal(err)
		}
		value, err := scripted.RunScript(`baked.answer + ":" + label + ":" + typeof [].sort`, "check.js")
		if err != nil || value.String() != "42:artemis:function" {
			t.Fatalf("scripted realm lost baked state: %v %v", value, err)
		}
		plain := NewContext(iso)
		pristine, err := plain.RunScript(`typeof baked + ":" + typeof label + ":" + typeof [].sort`, "plain.js")
		if err != nil || pristine.String() != "undefined:undefined:function" {
			t.Fatalf("plain realm is not pristine: %v %v", pristine, err)
		}
		if _, err := NewContextFromSnapshot(iso, 1); err == nil {
			t.Fatal("a missing snapshot context opened")
		}
		scripted.Close()
		plain.Close()
		iso.Dispose()
	}
}
