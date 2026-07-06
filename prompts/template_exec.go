// Package prompts provides the 9 AgentScope browser agent prompt templates
// (spec ss28.11) as Go string constants. This file implements the
// TemplateExecutor that integrates template selection with browser skill
// execution (spec L4338-L4346): the agent selects one of the 9 templates
// per situation, renders it with variables, and applies token
// optimization (pure reasoning first, chunked observation, early
// completion).
package prompts

import (
	"context"
	"fmt"
	"strings"
)

// TemplateSelector selects the prompt template to use for a given
// situation string. Implementations map situation keywords (e.g.
// "decompose", "observe", "form") to a PromptType.
type TemplateSelector func(ctx context.Context, situation string) (PromptType, error)

// TemplateExecutor integrates template selection with browser skill
// execution. It owns the 9 prompt templates, a selector function, and a
// token budget used to decide when to fall back to pure reasoning.
type TemplateExecutor struct {
	templates map[PromptType]*PromptTemplate
	selector  TemplateSelector
	maxTokens int
}

// NewTemplateExecutor constructs a TemplateExecutor that loads all 9
// templates from the prompts package and installs DefaultSelector as
// the initial selector. The default token budget is 20000 tokens
// (matching the spec's 80KB / ~20000-token page threshold).
func NewTemplateExecutor() *TemplateExecutor {
	all := AllTemplates()
	tmpls := make(map[PromptType]*PromptTemplate, len(all))
	for i := range all {
		t := all[i]
		tmpls[t.Type] = &t
	}
	ex := &TemplateExecutor{
		templates: tmpls,
		maxTokens: 20000,
	}
	ex.selector = func(ctx context.Context, situation string) (PromptType, error) {
		return DefaultSelector(situation), nil
	}
	return ex
}

// SetSelector replaces the template selector. Passing nil resets to the
// default selector.
func (e *TemplateExecutor) SetSelector(s TemplateSelector) {
	if s == nil {
		e.selector = func(ctx context.Context, situation string) (PromptType, error) {
			return DefaultSelector(situation), nil
		}
		return
	}
	e.selector = s
}

// SetMaxTokens overrides the token budget used by the pure-reasoning
// fallback.
func (e *TemplateExecutor) SetMaxTokens(n int) {
	if n > 0 {
		e.maxTokens = n
	}
}

// Execute selects a template via the configured selector, renders it
// with the given variables, and returns the rendered prompt. When the
// situation does not require visual inspection and the estimated page
// token count exceeds the budget, Execute uses PromptPureReasoning first
// (token optimization per spec L4338-L4346).
func (e *TemplateExecutor) Execute(ctx context.Context, situation string, variables map[string]string) (string, error) {
	if e == nil {
		return "", fmt.Errorf("prompts: TemplateExecutor is nil")
	}

	pageTokens := 0
	if v, ok := variables["page_estimated_tokens"]; ok {
		fmt.Sscanf(v, "%d", &pageTokens)
	}

	pt, err := e.selector(ctx, situation)
	if err != nil {
		return "", fmt.Errorf("prompts: template selection failed: %w", err)
	}

	// Token optimization: if the page is large and the situation does not
	// require visual inspection, prefer pure reasoning.
	if ShouldUsePureReasoning(situation, pageTokens) {
		pt = PromptPureReasoning
	}

	tmpl, ok := e.templates[pt]
	if !ok {
		return "", fmt.Errorf("prompts: template %s not loaded", pt)
	}
	return RenderTemplate(tmpl.Template, variables), nil
}

// RenderTemplate replaces {variable} substitution tokens in template
// with the corresponding values from variables. Unknown tokens are left
// intact so that nested templates or literal braces are not corrupted.
func RenderTemplate(template string, variables map[string]string) string {
	if len(variables) == 0 {
		return template
	}
	out := template
	// Apply longest keys first so that overlapping names (e.g.
	// {current_subtask} vs {current}) do not clobber each other. The
	// token syntax includes the braces, so we replace "{key}".
	keys := sortedKeysDesc(variables)
	for _, k := range keys {
		token := "{" + k + "}"
		out = strings.ReplaceAll(out, token, variables[k])
	}
	return out
}

// sortedKeysDesc returns the variable keys sorted by descending length.
func sortedKeysDesc(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Insertion sort by descending length; variable maps are small (<= 10
	// entries) so a simple sort is sufficient and allocation-free beyond
	// the key slice.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && len(keys[j]) > len(keys[j-1]); j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// DefaultSelector maps a situation string to a PromptType. Matching is
// keyword-based and case-insensitive: the situation is scanned for any
// of the keywords associated with each template. When no keyword
// matches, PromptPureReasoning is returned (token optimization default).
func DefaultSelector(situation string) PromptType {
	s := strings.ToLower(strings.TrimSpace(situation))
	if s == "" {
		return PromptPureReasoning
	}
	// Order matters: more specific situations are checked first so that
	// "validate_decomposition" maps to the reflection template rather
	// than the decomposition template.
	switch {
	case containsAny(s, "validate_decomposition", "decompose_reflect"):
		return PromptDecomposeReflection
	case containsAny(s, "decompose", "break_down", "task_decomposition"):
		return PromptTaskDecomposition
	case containsAny(s, "system", "start"):
		return PromptSystem
	case containsAny(s, "pure_reason", "reason"):
		return PromptPureReasoning
	case containsAny(s, "observe", "chunk"):
		return PromptObserveReasoning
	case containsAny(s, "revise", "not_done", "subtask_revision"):
		return PromptSubtaskRevision
	case containsAny(s, "form", "fill_form"):
		return PromptFormFilling
	case containsAny(s, "download", "file"):
		return PromptFileDownload
	case containsAny(s, "summarize", "final"):
		return PromptSummarizeTask
	}
	return PromptPureReasoning
}

// containsAny reports whether s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// EstimateTokens returns a rough token estimate for text: len(text)/4.
// This matches the common heuristic that ~4 characters correspond to one
// token for English text.
func EstimateTokens(text string) int {
	return len(text) / 4
}

// ShouldUsePureReasoning reports whether pure reasoning should be used
// instead of a visual/observation template. It returns true when the
// estimated page token count exceeds 20000 (the spec's 80KB threshold)
// and the situation does not require visual inspection (i.e. it is not
// an observe, form, download, or summarize situation).
func ShouldUsePureReasoning(situation string, pageEstimatedTokens int) bool {
	if pageEstimatedTokens <= 20000 {
		return false
	}
	s := strings.ToLower(strings.TrimSpace(situation))
	if s == "" {
		return true
	}
	visualKeywords := []string{"observe", "chunk", "form", "fill_form", "download", "file", "summarize", "final"}
	for _, kw := range visualKeywords {
		if strings.Contains(s, kw) {
			return false
		}
	}
	return true
}
