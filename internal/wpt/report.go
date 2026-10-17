package wpt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Christopher-Schulze/Artemis/engine"
)

const maxReportBytes = 1 << 20

type harnessReport struct {
	Status  *int         `json:"harnessStatus"`
	Message string       `json:"harnessMessage"`
	Tests   []reportTest `json:"tests"`
}

type reportTest struct {
	Name    string `json:"name"`
	Status  *int   `json:"status"`
	Message string `json:"message"`
}

func awaitReport(ctx context.Context, page *engine.Page, tc TestCase) (Result, error) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		value, err := page.Eval(ctx, "typeof __wptResults === 'undefined' ? '' : JSON.stringify(__wptResults)")
		if err != nil {
			return Result{}, fmt.Errorf("read WPT completion %s: %w", tc.Path, err)
		}
		raw := value.String()
		value.Release()
		if raw != "" && raw != "null" && raw != "undefined" {
			return parseReport(raw, tc)
		}
		select {
		case <-ctx.Done():
			return Result{}, fmt.Errorf("wpt %s/%s: harness produced no completion: %w", tc.Path, tc.Name, ctx.Err())
		case <-ticker.C:
		}
	}
}

func parseReport(raw string, tc TestCase) (Result, error) {
	if len(raw) > maxReportBytes || !utf8.ValidString(raw) {
		return Result{}, fmt.Errorf("wpt: report exceeds byte limit or is not UTF-8")
	}
	if err := validateJSONKeys(json.NewDecoder(strings.NewReader(raw)), 0); err != nil {
		return Result{}, fmt.Errorf("wpt: invalid report: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var report harnessReport
	if err := decoder.Decode(&report); err != nil {
		return Result{}, fmt.Errorf("wpt: decode report: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return Result{}, fmt.Errorf("wpt: report contains trailing data")
	}
	if len(report.Message) > 4096 || len(report.Tests) == 0 || len(report.Tests) > 1024 {
		return Result{}, fmt.Errorf("wpt: invalid report message or test count")
	}
	if report.Status == nil {
		return Result{}, fmt.Errorf("wpt: harness completion status is missing")
	}
	if *report.Status != 0 {
		return Result{}, fmt.Errorf("wpt: harness did not complete OK: status=%s message=%q", harnessStatus(*report.Status), report.Message)
	}
	return selectReportTest(report, tc)
}

func selectReportTest(report harnessReport, tc TestCase) (Result, error) {
	seen := make(map[string]bool, len(report.Tests))
	var selected Result
	for _, test := range report.Tests {
		if len(test.Name) > 4096 || len(test.Message) > 4096 {
			return Result{}, fmt.Errorf("wpt: test identity or message exceeds limit")
		}
		if test.Name == "" || test.Status == nil || seen[test.Name] {
			return Result{}, fmt.Errorf("wpt: incomplete or duplicate test %q", test.Name)
		}
		seen[test.Name] = true
		result := Result{Path: tc.Path, Name: test.Name, Status: testStatus(*test.Status), Message: test.Message, HarnessStatus: "OK"}
		if err := result.Validate(); err != nil {
			return Result{}, err
		}
		if test.Name == tc.Name {
			selected = result
		}
	}
	if selected.Name == "" {
		return Result{}, fmt.Errorf("wpt %s/%s: completed harness did not report requested test", tc.Path, tc.Name)
	}
	return selected, nil
}

// validateJSONKeys rejects duplicate fields before typed decoding can discard
// an earlier conflicting status. The report schema needs only four levels.
func validateJSONKeys(decoder *json.Decoder, depth int) error {
	if depth > 4 {
		return fmt.Errorf("report nesting exceeds schema")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	if delimiter != '{' && delimiter != '[' {
		return fmt.Errorf("unexpected delimiter %q", delimiter)
	}
	return validateJSONMembers(decoder, delimiter, depth)
}

func validateJSONMembers(decoder *json.Decoder, delimiter json.Delim, depth int) error {
	seen := make(map[string]bool)
	for decoder.More() {
		if delimiter == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, valid := key.(string)
			if !valid || seen[name] {
				return fmt.Errorf("invalid or duplicate field %q", name)
			}
			seen[name] = true
		}
		if err := validateJSONKeys(decoder, depth+1); err != nil {
			return err
		}
	}
	_, err := decoder.Token()
	return err
}
