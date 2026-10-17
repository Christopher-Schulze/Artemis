package wpt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/internal/fixture"
)

func TestReportRequiresCompleteUnambiguousEvidence(t *testing.T) {
	tc := TestCase{Path: "a.html", Name: "actual", Expected: OutcomePass, CapabilityID: "renderless.javascript"}
	valid := `{"harnessStatus":0,"harnessMessage":"","tests":[{"name":"actual","status":0,"message":""}]}`
	result, err := parseReport(valid, tc)
	if err != nil || result.Status != "PASS" || result.HarnessStatus != "OK" {
		t.Fatalf("actual complete report: %+v %v", result, err)
	}
	cases := []struct{ name, raw string }{
		{"missing harness status", strings.Replace(valid, `"harnessStatus":0,`, "", 1)},
		{"null harness status", strings.Replace(valid, `"harnessStatus":0`, `"harnessStatus":null`, 1)},
		{"failed harness", strings.Replace(valid, `"harnessStatus":0`, `"harnessStatus":1`, 1)},
		{"unknown harness", strings.Replace(valid, `"harnessStatus":0`, `"harnessStatus":8`, 1)},
		{"missing test status", strings.Replace(valid, `,"status":0`, "", 1)},
		{"null test status", strings.Replace(valid, `"status":0`, `"status":null`, 1)},
		{"unknown test status", strings.Replace(valid, `"status":0`, `"status":8`, 1)},
		{"duplicate harness field", strings.Replace(valid, `"harnessStatus":0`, `"harnessStatus":1,"harnessStatus":0`, 1)},
		{"duplicate test field", strings.Replace(valid, `"status":0`, `"status":1,"status":0`, 1)},
		{"duplicate test", strings.Replace(valid, `}]}`, `},{"name":"actual","status":0,"message":""}]}`, 1)},
		{"missing requested test", strings.Replace(valid, `"actual"`, `"another"`, 1)},
		{"unknown field", strings.Replace(valid, `"harnessStatus":0`, `"harnessStatus":0,"extra":true`, 1)},
		{"trailing data", valid + `{}`},
		{"oversized report", strings.Repeat(" ", maxReportBytes+1)},
		{"invalid UTF-8", string([]byte{0xff})},
		{"oversized message", strings.Replace(valid, `"harnessMessage":""`, `"harnessMessage":"`+strings.Repeat("x", 4097)+`"`, 1)},
		{"invalid unselected test", strings.Replace(valid, `}]}`, `},{"name":"other","status":9,"message":""}]}`, 1)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got, err := parseReport(test.raw, tc); err == nil || got.Status != "" {
				t.Fatalf("invalid evidence returned %+v %v", got, err)
			}
		})
	}
	failed, err := parseReport(strings.Replace(valid, `"status":0`, `"status":1`, 1), tc)
	if err != nil || failed.Status != "FAIL" {
		t.Fatalf("actual failing subtest was lost: %+v %v", failed, err)
	}
}

func TestServedWPTAssetsAreUnmodifiedEmbeddedBytes(t *testing.T) {
	server := fixture.NewServer()
	defer server.Close()
	manifest, content, err := DefaultFixture()
	if err != nil {
		t.Fatal(err)
	}
	input, err := decodeFixture(manifest, content)
	if err != nil {
		t.Fatal(err)
	}
	runner := &Runner{Server: server, input: input, manifest: manifest}
	if err := runner.registerFiles(); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	err = fs.WalkDir(testdata, "testdata/wpt", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		expected, err := testdata.ReadFile(path)
		if err != nil {
			return err
		}
		response, err := client.Get(server.URL(strings.TrimPrefix(path, "testdata/wpt/")))
		if err != nil {
			return err
		}
		actual, readErr := io.ReadAll(io.LimitReader(response.Body, int64(len(expected))+1))
		if err := errors.Join(readErr, response.Body.Close()); err != nil {
			return err
		}
		if response.StatusCode != 200 || !bytes.Equal(actual, expected) {
			t.Errorf("asset %s was altered: HTTP %d bytes=%d expected=%d", path, response.StatusCode, len(actual), len(expected))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMissingCompletionPreservesOwnerDeadline(t *testing.T) {
	runner, err := NewRunner()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := runner.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := runner.Server.RegisterHTML("/deadline.html", "<html><body>No harness</body></html>"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	result, err := runner.RunCase(ctx, TestCase{Path: "deadline.html", Name: "missing", Expected: OutcomePass, CapabilityID: "renderless.javascript"})
	if !errors.Is(err, context.DeadlineExceeded) || result.Status != "" {
		t.Fatalf("missing completion lost deadline or supplied evidence: %+v %v", result, err)
	}
}

func TestSubsetRejectsAliasesDuplicatesBoundsAndForgedProvenance(t *testing.T) {
	for _, path := range []string{"../a.html", "/a.html", "a//b.html", "a.html?x=1", "a.html#x", "a\\b.html", "a.js", strings.Repeat("x", 1025) + ".html"} {
		tc := DefaultSubset().Tests[0]
		tc.Path = path
		if err := tc.Validate(); err == nil {
			t.Errorf("invalid local path accepted: %q", path)
		}
	}
	duplicate := DefaultSubset()
	duplicate.Tests = append(duplicate.Tests, duplicate.Tests[0])
	duplicate.Tests[len(duplicate.Tests)-1].Expected = OutcomeFail
	if err := duplicate.Validate(); err == nil {
		t.Fatal("conflicting duplicate test identity accepted")
	}
	oversized := DefaultSubset()
	oversized.Tests = make([]TestCase, 1025)
	if err := oversized.Validate(); err == nil {
		t.Fatal("excess test count accepted")
	}
	runner, err := NewRunner()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := runner.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, field := range []string{"revision", "origin", "asset"} {
		subset := DefaultSubset()
		switch field {
		case "revision":
			subset.Revision = "forged"
		case "origin":
			subset.OriginURL = "https://different.invalid"
		case "asset":
			subset.Tests[0].Path = "absent.html"
		}
		results, err := runner.Run(context.Background(), subset)
		if err == nil || len(results) != 0 {
			t.Fatalf("forged %s supplied evidence: %+v %v", field, results, err)
		}
	}
}
