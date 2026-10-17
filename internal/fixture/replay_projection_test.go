package fixture

import (
	"strings"
	"testing"
)

func TestReplayProjectionPreservesFullLargeAssertionsWithoutWideningFixtureMetadata(t *testing.T) {
	text := strings.Repeat("complete captured text ", 2000)
	result := CrossResult{StatusCode: 200, Title: "Large", Text: text, HTML: "<html><title>Large</title><body><p>" + text + "</p></body></html>"}
	record, err := CaptureReplay(Scenario{ID: "large-snapshot", Kind: KindHTML}, "renderless", result, 0, SystemMetadata(), DefaultRedactionConfig(), "")
	if err != nil {
		t.Fatal(err)
	}
	regression, err := record.AsRegression()
	if err != nil || len(regression.Expect.Contains) != 1 || regression.Expect.Contains[0] != text {
		t.Fatalf("complete captured assertion was narrowed or lost: %v", err)
	}
	_, admittedBytes, err := prepareScenario(regression)
	if err != nil || admittedBytes < int64(len(regression.HTML)+len(text)) {
		t.Fatalf("snapshot assertion bytes escaped the actual registration budget: %d %v", admittedBytes, err)
	}
	normal := regression
	normal.inertReplay = false
	if _, _, err := prepareScenario(normal); err == nil {
		t.Fatal("ordinary fixture metadata limit widened")
	}
	oversized := regression
	oversized.Description = strings.Repeat("x", (16<<10)+1)
	if _, _, err := prepareScenario(oversized); err == nil {
		t.Fatal("inert projection widened unrelated metadata")
	}
	oversized = regression
	oversized.Expect.Contains = []string{text, strings.Repeat("x", maxReplayTextBytes)}
	if _, _, err := prepareScenario(oversized); err == nil {
		t.Fatal("aggregate snapshot assertion bound bypassed")
	}
	if recaptured, err := CaptureReplay(regression, "renderless", result, 0, SystemMetadata(), DefaultRedactionConfig(), ""); err == nil || recaptured != nil {
		t.Fatal("private projection flag published a source that could not pass ordinary serialized-source admission")
	}
}
