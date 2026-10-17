package telemetry

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestTraceTextOwnsBoundedBytesAndCollectorMarksTruncation(t *testing.T) {
	source := strings.Repeat("x", 2*traceTextLimit)
	bounded := truncateTraceText(source)
	if len(bounded) != traceTextLimit || bounded != source[:traceTextLimit] || unsafe.StringData(bounded) == unsafe.StringData(source) {
		t.Fatal("clipped text retained the original large backing string")
	}
	c := newTraceEventCollector(TargetIdentity{TargetID: "target", SessionID: "session"})
	recordResponseEvent(t, c, "Runtime.consoleAPICalled", fmt.Sprintf(`{"type":"log","args":[{"value":%q}]}`, source))
	evidence := c.snapshot()
	if len(evidence.Console) != 1 || len(evidence.Console[0].Text) != traceTextLimit || !evidence.Truncated {
		t.Fatalf("bounded console omission was not explicit: %+v", evidence)
	}
	c = newTraceEventCollector(TargetIdentity{TargetID: "target", SessionID: "session"})
	for index := 0; index < traceDebugEntryLimit; index++ {
		recordResponseEvent(t, c, "Runtime.exceptionThrown", fmt.Sprintf(`{"exceptionDetails":{"text":%q,"exception":{"description":%q}}}`, source, source))
	}
	before := c.debugBytes
	recordResponseEvent(t, c, "Runtime.consoleAPICalled", `{"type":"log","args":[{"value":"one more"}]}`)
	if before != traceDebugBytesLimit || c.debugBytes != before || len(c.console) != 0 || !c.truncated {
		t.Fatalf("aggregate retained text exceeded its byte limit: before=%d after=%d console=%d", before, c.debugBytes, len(c.console))
	}
}

func TestTraceSourceDecodeBoundsAndInvalidTail(t *testing.T) {
	for _, encoded := range []bool{false, true} {
		for _, size := range []int{0, traceSourceBytesLimit, traceSourceBytesLimit + 1, 2 * traceSourceBytesLimit} {
			data := bytes.Repeat([]byte("a"), size)
			content := string(data)
			if encoded {
				content = base64.StdEncoding.EncodeToString(data)
			}
			got, truncated, err := decodeTraceSource(content, encoded)
			if err != nil || truncated != (size > traceSourceBytesLimit) || !bytes.Equal(got, data[:min(size, traceSourceBytesLimit)]) {
				t.Fatalf("encoded=%v size=%d: bytes=%d truncated=%v err=%v", encoded, size, len(got), truncated, err)
			}
		}
	}
	content := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("a"), 2*traceSourceBytesLimit)) + "!"
	if data, _, err := decodeTraceSource(content, true); err == nil || data != nil {
		t.Fatal("invalid base64 after the retained prefix was accepted")
	}
}

func TestTraceResourceFrameTraversalIsBoundedAndOrdered(t *testing.T) {
	root := traceResourceFrame{ChildFrames: make([]traceResourceFrame, traceDebugEntryLimit+1)}
	root.Frame.ID = "root"
	for index := range root.ChildFrames {
		root.ChildFrames[index].Frame.ID = fmt.Sprint(index)
	}
	frames, truncated := flattenTraceResourceFrames(root)
	if !truncated || len(frames) != traceDebugEntryLimit || frames[0].Frame.ID != "root" {
		t.Fatalf("frame count was not bounded: count=%d truncated=%v", len(frames), truncated)
	}
	for index, frame := range frames[1:] {
		if frame.Frame.ID != fmt.Sprint(index) {
			t.Fatal("bounded traversal reordered the retained frames")
		}
	}
}

func TestTraceArtifactBoundsAndDetachedOwnership(t *testing.T) {
	config := DefaultTraceRecordConfig(t.TempDir())
	config.Sources = true
	r := NewTraceRecorder(config)
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("a"), TraceEntryBytesLimit)
	if err := r.AddSnapshot(data); err != nil {
		t.Fatalf("exact entry bound rejected: %v", err)
	}
	data[0] = 'b'
	if r.snapshots[0][0] != 'a' {
		t.Fatal("caller mutated retained artifact")
	}
	for _, add := range []func([]byte) error{r.AddSnapshot, r.AddScreenshot, r.AddSource} {
		before := r.retainedBytes
		if err := add(append(data, 'x')); err == nil || r.retainedBytes != before || r.artifactCount != 1 {
			t.Fatal("oversized artifact admitted or changed retained state")
		}
	}
	for r.retainedBytes < TraceTotalBytesLimit {
		if err := r.AddSource(data); err != nil {
			t.Fatalf("exact aggregate bound rejected: %v", err)
		}
	}
	if err := r.AddScreenshot([]byte("x")); err == nil || r.retainedBytes != TraceTotalBytesLimit {
		t.Fatal("aggregate bound bypassed")
	}
	if _, err := r.Stop(); err == nil {
		t.Fatal("full payload plus metadata bypassed decoded ZIP aggregate limit")
	}
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	if r.retainedBytes != 0 || r.artifactCount != 0 {
		t.Fatal("new recording inherited consumed budget")
	}
	for index := 0; index < TraceArtifactLimit; index++ {
		if err := r.AddScreenshot(nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.AddSnapshot(nil); err == nil || r.artifactCount != TraceArtifactLimit {
		t.Fatal("empty artifacts bypassed count bound")
	}
	if _, err := r.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestTracePublicationCannotOverwriteExistingArchive(t *testing.T) {
	config := DefaultTraceRecordConfig(t.TempDir())
	stamp := time.Unix(1000, 123456789)
	first := NewTraceRecorder(config)
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	first.startedAt = stamp
	if err := first.AddSnapshot([]byte("original")); err != nil {
		t.Fatal(err)
	}
	path, err := first.Stop()
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	second := NewTraceRecorder(config)
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	second.startedAt = stamp
	if err := second.AddSnapshot([]byte("replacement")); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Stop(); !errors.Is(err, os.ErrExist) {
		t.Fatalf("archive collision did not fail exclusively: %v", err)
	}
	retained, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(retained, original) {
		t.Fatal("existing archive was changed")
	}
	entries, err := os.ReadDir(config.TraceDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("publication left an owned temporary file: %v %v", entries, err)
	}
}

func TestTraceZipWriteFailurePreservesForeignFileAndCleansOwnedPartial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewTraceRecorder(DefaultTraceRecordConfig(t.TempDir()))
	if err := r.writeZip(path); !errors.Is(err, os.ErrExist) {
		t.Fatalf("exclusive temporary open returned %v", err)
	}
	retained, err := os.ReadFile(path)
	if err != nil || string(retained) != "original" {
		t.Fatal("failed temporary creation removed foreign data")
	}
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, TraceEntryBytesLimit)
	if _, err := rand.New(rand.NewSource(1)).Read(data); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < TraceTotalBytesLimit/TraceEntryBytesLimit; index++ {
		if err := r.AddSnapshot(data); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Stop(); err == nil || !strings.Contains(err.Error(), "trace ZIP exceeds") {
		t.Fatalf("incompressible ZIP did not hit serialized archive limit: %v", err)
	}
	entries, err := os.ReadDir(r.config.TraceDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed ZIP published or leaked a partial file: %v %v", entries, err)
	}
}

func TestTraceDebugAdmissionRetainsIdentityAndBudget(t *testing.T) {
	identity := TargetIdentity{TargetID: "target", SessionID: "session"}
	r, err := NewTraceRecorderWithTarget(DefaultTraceRecordConfig(t.TempDir()), identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	value := TraceDebugEvidence{Target: identity, Console: []TraceConsoleEntry{{Text: "original"}}}
	if err := r.SetDebugEvidence(value); err != nil {
		t.Fatal(err)
	}
	value.Console[0].Text = "changed"
	if r.debug.Console[0].Text != "original" {
		t.Fatal("debug evidence aliases caller")
	}
	before := r.retainedBytes
	for _, bad := range []TraceDebugEvidence{
		{Target: TargetIdentity{TargetID: "foreign", SessionID: "session"}},
		{Target: identity, Network: make([]TraceNetworkEntry, traceDebugEntryLimit+1)},
		{Target: identity, Console: []TraceConsoleEntry{{Text: string(bytes.Repeat([]byte("x"), traceTextLimit+1))}}},
	} {
		if err := r.SetDebugEvidence(bad); err == nil || r.retainedBytes != before || r.debug.Console[0].Text != "original" {
			t.Fatal("invalid debug evidence replaced admitted evidence")
		}
	}
	if _, err := r.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestReadTraceZipRejectsExpansionCountsAndDuplicateNames(t *testing.T) {
	for _, scenario := range []string{"entry", "total", "count", "duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input.zip")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			archive := zip.NewWriter(file)
			count, size := 1, TraceEntryBytesLimit+1
			switch scenario {
			case "total":
				count, size = TraceTotalBytesLimit/TraceEntryBytesLimit+1, TraceEntryBytesLimit
			case "count":
				count, size = TraceArtifactLimit+traceFixedEntries+1, 0
			case "duplicate":
				count, size = 2, 1
			}
			for index := 0; index < count; index++ {
				name := fmt.Sprintf("entry-%d", index)
				if scenario == "duplicate" {
					name = "duplicate"
				}
				entry, err := archive.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := io.Copy(entry, bytes.NewReader(make([]byte, size))); err != nil {
					t.Fatal(err)
				}
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			if entries, err := ReadTraceZip(path); err == nil || entries != nil {
				t.Fatal("unsafe archive read succeeded or exposed partial entries")
			}
		})
	}
}
