package prompts

import (
	"context"
	"strings"
	"testing"
)

func TestNewTemplateExecutorLoadsAllTemplates(t *testing.T) {
	ex := NewTemplateExecutor()
	all := AllPromptTypes()
	if len(ex.templates) != len(all) {
		t.Fatalf("expected %d templates, got %d", len(all), len(ex.templates))
	}
	for _, pt := range all {
		tmpl, ok := ex.templates[pt]
		if !ok {
			t.Fatalf("template %s not loaded", pt)
		}
		if tmpl == nil {
			t.Fatalf("template %s is nil", pt)
		}
		if tmpl.Template == "" {
			t.Fatalf("template %s content is empty", pt)
		}
		if tmpl.Type != pt {
			t.Fatalf("template %s type mismatch: %v", pt, tmpl.Type)
		}
	}
	if ex.maxTokens != 20000 {
		t.Fatalf("default maxTokens = %d, want 20000", ex.maxTokens)
	}
	if ex.selector == nil {
		t.Fatal("default selector must be set")
	}
}

func TestDefaultSelectorAllSituations(t *testing.T) {
	cases := []struct {
		situation string
		want      PromptType
	}{
		{"system", PromptSystem},
		{"start", PromptSystem},
		{"decompose", PromptTaskDecomposition},
		{"break_down", PromptTaskDecomposition},
		{"task_decomposition", PromptTaskDecomposition},
		{"validate_decomposition", PromptDecomposeReflection},
		{"decompose_reflect", PromptDecomposeReflection},
		{"reason", PromptPureReasoning},
		{"pure_reason", PromptPureReasoning},
		{"observe", PromptObserveReasoning},
		{"chunk", PromptObserveReasoning},
		{"revise", PromptSubtaskRevision},
		{"not_done", PromptSubtaskRevision},
		{"subtask_revision", PromptSubtaskRevision},
		{"form", PromptFormFilling},
		{"fill_form", PromptFormFilling},
		{"download", PromptFileDownload},
		{"file", PromptFileDownload},
		{"summarize", PromptSummarizeTask},
		{"final", PromptSummarizeTask},
	}
	for _, c := range cases {
		if got := DefaultSelector(c.situation); got != c.want {
			t.Errorf("DefaultSelector(%q) = %s, want %s", c.situation, got, c.want)
		}
	}
}

func TestDefaultSelectorUnknown(t *testing.T) {
	if got := DefaultSelector("something_unknown"); got != PromptPureReasoning {
		t.Fatalf("DefaultSelector(unknown) = %s, want PromptPureReasoning", got)
	}
	if got := DefaultSelector(""); got != PromptPureReasoning {
		t.Fatalf("DefaultSelector('') = %s, want PromptPureReasoning", got)
	}
}

func TestDefaultSelectorCaseInsensitive(t *testing.T) {
	if got := DefaultSelector("DECOMPOSE"); got != PromptTaskDecomposition {
		t.Fatalf("DefaultSelector(DECOMPOSE) = %s, want PromptTaskDecomposition", got)
	}
	if got := DefaultSelector("Observe"); got != PromptObserveReasoning {
		t.Fatalf("DefaultSelector(Observe) = %s, want PromptObserveReasoning", got)
	}
}

func TestDefaultSelectorSpecificBeforeGeneric(t *testing.T) {
	// "validate_decomposition" must map to reflection, not decomposition.
	if got := DefaultSelector("validate_decomposition"); got != PromptDecomposeReflection {
		t.Fatalf("DefaultSelector(validate_decomposition) = %s, want PromptDecomposeReflection", got)
	}
}

func TestRenderTemplateVariableReplacement(t *testing.T) {
	tmpl := "Hello {name}, subtask: {current_subtask}"
	vars := map[string]string{
		"name":            "Alice",
		"current_subtask": "click search",
	}
	got := RenderTemplate(tmpl, vars)
	want := "Hello Alice, subtask: click search"
	if got != want {
		t.Fatalf("RenderTemplate() = %q, want %q", got, want)
	}
}

func TestRenderTemplateObserveVariables(t *testing.T) {
	tmpl, err := GetPrompt(PromptObserveReasoning)
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{
		"previous_chunkwise_information": "found pricing section",
		"i":                              "2",
		"total_pages":                    "5",
	}
	got := RenderTemplate(tmpl, vars)
	if !strings.Contains(got, "found pricing section") {
		t.Fatal("rendered observe prompt must contain previous_chunkwise_information value")
	}
	if !strings.Contains(got, "chunk 2 of 5") {
		t.Fatalf("rendered observe prompt must contain 'chunk 2 of 5': %q", got)
	}
}

func TestRenderTemplateSystemName(t *testing.T) {
	tmpl, err := GetPrompt(PromptSystem)
	if err != nil {
		t.Fatal(err)
	}
	got := RenderTemplate(tmpl, map[string]string{"name": "OmniBot"})
	if !strings.Contains(got, "OmniBot") {
		t.Fatal("rendered system prompt must contain the name variable")
	}
}

func TestRenderTemplateNoVariables(t *testing.T) {
	tmpl := "no substitution tokens here"
	if got := RenderTemplate(tmpl, nil); got != tmpl {
		t.Fatalf("RenderTemplate with nil vars = %q, want %q", got, tmpl)
	}
	if got := RenderTemplate(tmpl, map[string]string{}); got != tmpl {
		t.Fatalf("RenderTemplate with empty vars = %q, want %q", got, tmpl)
	}
}

func TestRenderTemplateUnknownPlaceholderLeftIntact(t *testing.T) {
	tmpl := "hello {name} {unknown_var}"
	got := RenderTemplate(tmpl, map[string]string{"name": "X"})
	want := "hello X {unknown_var}"
	if got != want {
		t.Fatalf("RenderTemplate() = %q, want %q", got, want)
	}
}

func TestRenderTemplateOverlappingKeys(t *testing.T) {
	// {current_subtask} must be replaced before {current} would match a
	// prefix; since we use full "{key}" replacement, both should resolve.
	tmpl := "{current} and {current_subtask}"
	vars := map[string]string{
		"current":         "A",
		"current_subtask": "B",
	}
	got := RenderTemplate(tmpl, vars)
	want := "A and B"
	if got != want {
		t.Fatalf("RenderTemplate() = %q, want %q", got, want)
	}
}

func TestExecuteWithValidSituation(t *testing.T) {
	ex := NewTemplateExecutor()
	ctx := context.Background()
	out, err := ex.Execute(ctx, "decompose", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Indivisible") {
		t.Fatalf("Execute(decompose) must render task decomposition template: %q", out)
	}
}

func TestExecuteWithVariables(t *testing.T) {
	ex := NewTemplateExecutor()
	ctx := context.Background()
	out, err := ex.Execute(ctx, "start", map[string]string{"name": "TestBot"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "TestBot") {
		t.Fatalf("Execute(start) must render name variable: %q", out)
	}
}

func TestExecuteObserveWithVariables(t *testing.T) {
	ex := NewTemplateExecutor()
	ctx := context.Background()
	out, err := ex.Execute(ctx, "observe", map[string]string{
		"previous_chunkwise_information": "prev chunk data",
		"i":                              "3",
		"total_pages":                    "10",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "prev chunk data") {
		t.Fatal("Execute(observe) must contain previous chunkwise info")
	}
	if !strings.Contains(out, "chunk 3 of 10") {
		t.Fatalf("Execute(observe) must contain chunk index: %q", out)
	}
}

func TestExecuteUnknownSituationDefaultsToPureReasoning(t *testing.T) {
	ex := NewTemplateExecutor()
	ctx := context.Background()
	out, err := ex.Execute(ctx, "totally_unknown_xyz", map[string]string{"current_subtask": "do thing"})
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "current subtask") {
		t.Fatalf("Execute(unknown) must render pure reasoning template: %q", out)
	}
	if !strings.Contains(lower, "reasoning") {
		t.Fatalf("Execute(unknown) must render pure reasoning template: %q", out)
	}
}

func TestExecuteTokenOptimizationPath(t *testing.T) {
	ex := NewTemplateExecutor()
	ctx := context.Background()
	// Large page + non-visual situation -> pure reasoning override.
	out, err := ex.Execute(ctx, "decompose", map[string]string{
		"current_subtask":       "find answer",
		"page_estimated_tokens": "25000",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Pure reasoning template contains "current subtask" and "reasoning".
	if !strings.Contains(out, "reasoning") {
		t.Fatalf("token optimization must select pure reasoning template: %q", out)
	}
}

func TestExecuteTokenOptimizationNotTriggeredForVisual(t *testing.T) {
	ex := NewTemplateExecutor()
	ctx := context.Background()
	// Large page but observe situation -> must NOT switch to pure reasoning.
	out, err := ex.Execute(ctx, "observe", map[string]string{
		"previous_chunkwise_information": "data",
		"i":                              "1",
		"total_pages":                    "2",
		"page_estimated_tokens":          "25000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "80KB") {
		t.Fatalf("observe situation must keep observe template even with large page: %q", out)
	}
}

func TestExecuteSelectorError(t *testing.T) {
	ex := NewTemplateExecutor()
	ex.SetSelector(func(ctx context.Context, situation string) (PromptType, error) {
		return 0, errSentinel
	})
	if _, err := ex.Execute(context.Background(), "x", nil); err == nil {
		t.Fatal("Execute must propagate selector error")
	}
}

func TestExecuteNilExecutor(t *testing.T) {
	var ex *TemplateExecutor
	if _, err := ex.Execute(context.Background(), "x", nil); err == nil {
		t.Fatal("nil executor Execute must error")
	}
}

func TestSetMaxTokens(t *testing.T) {
	ex := NewTemplateExecutor()
	ex.SetMaxTokens(1000)
	if ex.maxTokens != 1000 {
		t.Fatalf("maxTokens = %d, want 1000", ex.maxTokens)
	}
	ex.SetMaxTokens(0) // ignored
	if ex.maxTokens != 1000 {
		t.Fatalf("maxTokens must not change for non-positive input: %d", ex.maxTokens)
	}
}

func TestSetSelectorNilResetsDefault(t *testing.T) {
	ex := NewTemplateExecutor()
	ex.SetSelector(nil)
	if ex.selector == nil {
		t.Fatal("SetSelector(nil) must install default selector, not nil")
	}
	pt, err := ex.selector(context.Background(), "decompose")
	if err != nil {
		t.Fatal(err)
	}
	if pt != PromptTaskDecomposition {
		t.Fatalf("default selector after reset returned %s, want PromptTaskDecomposition", pt)
	}
}

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abcd", 1},
		{"abcdefgh", 2},
		{strings.Repeat("x", 80), 20},
	}
	for _, c := range cases {
		if got := EstimateTokens(c.in); got != c.want {
			t.Errorf("EstimateTokens(%d chars) = %d, want %d", len(c.in), got, c.want)
		}
	}
}

func TestShouldUsePureReasoningTrue(t *testing.T) {
	cases := []string{
		"decompose",
		"reason",
		"unknown situation",
		"",
	}
	for _, s := range cases {
		if !ShouldUsePureReasoning(s, 25000) {
			t.Errorf("ShouldUsePureReasoning(%q, 25000) must be true", s)
		}
	}
}

func TestShouldUsePureReasoningFalseBelowThreshold(t *testing.T) {
	if ShouldUsePureReasoning("decompose", 20000) {
		t.Fatal("ShouldUsePureReasoning must be false at threshold (20000)")
	}
	if ShouldUsePureReasoning("decompose", 10000) {
		t.Fatal("ShouldUsePureReasoning must be false below threshold")
	}
}

func TestShouldUsePureReasoningFalseForVisual(t *testing.T) {
	visual := []string{"observe", "chunk", "form", "fill_form", "download", "file", "summarize", "final"}
	for _, s := range visual {
		if ShouldUsePureReasoning(s, 25000) {
			t.Errorf("ShouldUsePureReasoning(%q, 25000) must be false (visual)", s)
		}
	}
}

// errSentinel is a sentinel error used by the selector-error test.
var errSentinel = &sentinelErr{}

type sentinelErr struct{}

func (e *sentinelErr) Error() string { return "sentinel selector error" }
