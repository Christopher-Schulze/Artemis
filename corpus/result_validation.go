package corpus

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/Christopher-Schulze/Artemis/fixtureasset"
	"github.com/Christopher-Schulze/Artemis/internal/fixture"
)

// MaxResultBytes bounds inert corpus observations before receipt encoding.
const MaxResultBytes = 1 << 20

// ValidateResult checks exact pinned input, bounded observations and every
// passing semantic assertion. It does not authenticate a runner or execute input.
func ValidateResult(ctx context.Context, manifest fixtureasset.Manifest, content []byte, result Result) error {
	if err := Validate(ctx, manifest, content); err != nil {
		return err
	}
	if err := validateResultBounds(result); err != nil {
		return err
	}
	if result.FixtureManifest.ID == "" {
		if err := validateAbsentResult(result); err != nil {
			return err
		}
		return ctx.Err()
	}
	expected, err := fixtureasset.Encode(manifest, content)
	if err != nil {
		return err
	}
	observed, err := fixtureasset.Encode(result.FixtureManifest, content)
	if err != nil || !bytes.Equal(expected, observed) {
		return fmt.Errorf("fixture corpus result: exact admitted manifest required")
	}
	if !result.Passed {
		return ctx.Err()
	}
	scenario, err := fixture.CorpusScenario(manifest, content)
	if err != nil {
		return err
	}
	if err := fixture.ValidateCrossResult(scenario, fixture.CrossResult{URL: result.URL, StatusCode: result.StatusCode,
		Title: result.Title, Text: result.Text, HTML: result.HTML, Markdown: result.Markdown,
		EvalResult: result.EvalResult, EvalErr: result.EvalError, Links: result.Links}); err != nil {
		return err
	}
	return ctx.Err()
}

func validateResultBounds(result Result) error {
	if result.DurationMs < 0 || (result.StatusCode != 0 && (result.StatusCode < 100 || result.StatusCode > 599)) || len(result.Links) > 4096 {
		return fmt.Errorf("fixture corpus result: invalid observed count, status or duration")
	}
	total := 0
	fields := []string{result.URL, result.Title, result.Text, result.HTML, result.Markdown, result.EvalResult, result.EvalError}
	for _, field := range append(fields, result.Links...) {
		if len(field) > MaxResultBytes-total || !utf8.ValidString(field) || strings.ContainsRune(field, 0) {
			return fmt.Errorf("fixture corpus result: bounded NUL-free UTF-8 observations required")
		}
		total += len(field)
	}
	return nil
}

func validateAbsentResult(result Result) error {
	// An attempted run can fail before owning a page. Only elapsed time may
	// accompany an absent observation; no partial identity may claim a page.
	result.DurationMs = 0
	if !reflect.ValueOf(result).IsZero() {
		return fmt.Errorf("fixture corpus result: absent observation contains invented identity or output")
	}
	return nil
}
