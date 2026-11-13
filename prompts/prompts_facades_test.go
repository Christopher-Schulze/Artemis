package prompts

import (
	"strings"
	"testing"
)

// ==================== system.go tests ====================

// TestSystemPrompt verifies system prompt content
// (system.go - base behavior).
func TestSystemPrompt(t *testing.T) {
	p := SystemPrompt()
	if p == "" {
		t.Error("system prompt should not be empty")
	}
	if !strings.Contains(p, "Web Using AI") {
		t.Error("system prompt should contain 'Web Using AI'")
	}
}

// TestSystemPromptType verifies prompt type
// (system.go).
func TestSystemPromptType(t *testing.T) {
	if SystemPromptType() != PromptSystem {
		t.Error("system prompt type should be PromptSystem")
	}
}

// TestSystemPromptTemplate verifies template
// (system.go - base behavior).
func TestSystemPromptTemplate(t *testing.T) {
	tmpl := SystemPromptTemplate()
	if tmpl.Name != "system" {
		t.Errorf("name: got %s, want system", tmpl.Name)
	}
	if tmpl.Type != PromptSystem {
		t.Error("type should be PromptSystem")
	}
	if tmpl.Template == "" {
		t.Error("template should not be empty")
	}
}

// ==================== decompose.go tests ====================

// TestDecomposePrompt verifies decomposition prompt
// (decompose.go - task decomposition).
func TestDecomposePrompt(t *testing.T) {
	p := DecomposePrompt()
	if p == "" {
		t.Error("decompose prompt should not be empty")
	}
	if !strings.Contains(p, "Decomposition") {
		t.Error("decompose prompt should contain 'Decomposition'")
	}
}

// TestDecomposePromptType verifies prompt type
// (decompose.go).
func TestDecomposePromptType(t *testing.T) {
	if DecomposePromptType() != PromptTaskDecomposition {
		t.Error("decompose prompt type should be PromptTaskDecomposition")
	}
}

// TestDecomposePromptTemplate verifies template
// (decompose.go - task decomposition).
func TestDecomposePromptTemplate(t *testing.T) {
	tmpl := DecomposePromptTemplate()
	if tmpl.Name != "task_decomposition" {
		t.Errorf("name: got %s, want task_decomposition", tmpl.Name)
	}
	if tmpl.Type != PromptTaskDecomposition {
		t.Error("type should be PromptTaskDecomposition")
	}
}

// TestDecomposeReflectionPrompt verifies reflection prompt
// (decompose.go - task decomposition).
func TestDecomposeReflectionPrompt(t *testing.T) {
	p := DecomposeReflectionPrompt()
	if p == "" {
		t.Error("reflection prompt should not be empty")
	}
}

// TestDecomposeReflectionPromptType verifies prompt type.
func TestDecomposeReflectionPromptType(t *testing.T) {
	if DecomposeReflectionPromptType() != PromptDecomposeReflection {
		t.Error("reflection type should be PromptDecomposeReflection")
	}
}

// TestDecomposeReflectionPromptTemplate verifies template.
func TestDecomposeReflectionPromptTemplate(t *testing.T) {
	tmpl := DecomposeReflectionPromptTemplate()
	if tmpl.Type != PromptDecomposeReflection {
		t.Error("type should be PromptDecomposeReflection")
	}
}

// ==================== observe.go tests ====================

// TestObservePrompt verifies observation prompt
// (observe.go - chunked observation).
func TestObservePrompt(t *testing.T) {
	p := ObservePrompt()
	if p == "" {
		t.Error("observe prompt should not be empty")
	}
	if !strings.Contains(p, "snapshot") {
		t.Error("observe prompt should contain 'snapshot'")
	}
}

// TestObservePromptType verifies prompt type
// (observe.go).
func TestObservePromptType(t *testing.T) {
	if ObservePromptType() != PromptObserveReasoning {
		t.Error("observe prompt type should be PromptObserveReasoning")
	}
}

// TestObservePromptTemplate verifies template
// (observe.go - chunked observation).
func TestObservePromptTemplate(t *testing.T) {
	tmpl := ObservePromptTemplate()
	if tmpl.Name != "observe_reasoning" {
		t.Errorf("name: got %s, want observe_reasoning", tmpl.Name)
	}
	if tmpl.Type != PromptObserveReasoning {
		t.Error("type should be PromptObserveReasoning")
	}
}

// TestPureReasoningPrompt verifies pure reasoning prompt
// (observe.go - chunked observation).
func TestPureReasoningPrompt(t *testing.T) {
	p := PureReasoningPrompt()
	if p == "" {
		t.Error("pure reasoning prompt should not be empty")
	}
}

// TestPureReasoningPromptType verifies prompt type.
func TestPureReasoningPromptType(t *testing.T) {
	if PureReasoningPromptType() != PromptPureReasoning {
		t.Error("pure reasoning type should be PromptPureReasoning")
	}
}

// TestPureReasoningPromptTemplate verifies template.
func TestPureReasoningPromptTemplate(t *testing.T) {
	tmpl := PureReasoningPromptTemplate()
	if tmpl.Type != PromptPureReasoning {
		t.Error("type should be PromptPureReasoning")
	}
}

// ==================== forms.go tests ====================

// TestFormFillingPrompt verifies form filling prompt
// (forms.go - form filling).
func TestFormFillingPrompt(t *testing.T) {
	p := FormFillingPrompt()
	if p == "" {
		t.Error("form filling prompt should not be empty")
	}
	if !strings.Contains(p, "form") {
		t.Error("form filling prompt should contain 'form'")
	}
}

// TestFormFillingPromptType verifies prompt type
// (forms.go).
func TestFormFillingPromptType(t *testing.T) {
	if FormFillingPromptType() != PromptFormFilling {
		t.Error("form filling type should be PromptFormFilling")
	}
}

// TestFormFillingPromptTemplate verifies template
// (forms.go - form filling).
func TestFormFillingPromptTemplate(t *testing.T) {
	tmpl := FormFillingPromptTemplate()
	if tmpl.Name != "form_filling" {
		t.Errorf("name: got %s, want form_filling", tmpl.Name)
	}
	if tmpl.Type != PromptFormFilling {
		t.Error("type should be PromptFormFilling")
	}
}

// ==================== download.go tests ====================

// TestFileDownloadPrompt verifies file download prompt
// (download.go - file download).
func TestFileDownloadPrompt(t *testing.T) {
	p := FileDownloadPrompt()
	if p == "" {
		t.Error("file download prompt should not be empty")
	}
}

// TestFileDownloadPromptType verifies prompt type
// (download.go).
func TestFileDownloadPromptType(t *testing.T) {
	if FileDownloadPromptType() != PromptFileDownload {
		t.Error("file download type should be PromptFileDownload")
	}
}

// TestFileDownloadPromptTemplate verifies template
// (download.go - file download).
func TestFileDownloadPromptTemplate(t *testing.T) {
	tmpl := FileDownloadPromptTemplate()
	if tmpl.Name != "file_download" {
		t.Errorf("name: got %s, want file_download", tmpl.Name)
	}
	if tmpl.Type != PromptFileDownload {
		t.Error("type should be PromptFileDownload")
	}
}

// ==================== summarize.go tests ====================

// TestSummarizePrompt verifies summarize prompt
// (summarize.go - task summarization).
func TestSummarizePrompt(t *testing.T) {
	p := SummarizePrompt()
	if p == "" {
		t.Error("summarize prompt should not be empty")
	}
}

// TestSummarizePromptType verifies prompt type
// (summarize.go).
func TestSummarizePromptType(t *testing.T) {
	if SummarizePromptType() != PromptSummarizeTask {
		t.Error("summarize type should be PromptSummarizeTask")
	}
}

// TestSummarizePromptTemplate verifies template
// (summarize.go - task summarization).
func TestSummarizePromptTemplate(t *testing.T) {
	tmpl := SummarizePromptTemplate()
	if tmpl.Name != "summarize_task" {
		t.Errorf("name: got %s, want summarize_task", tmpl.Name)
	}
	if tmpl.Type != PromptSummarizeTask {
		t.Error("type should be PromptSummarizeTask")
	}
}

// ==================== parity test ====================

// TestPromptsFacadesParity verifies all 6 spec-mandated files
// (system.go, decompose.go, observe.go, forms.go,
// download.go, summarize.go).
func TestPromptsFacadesParity(t *testing.T) {
	// 1. system.go - base behavior
	if SystemPrompt() == "" {
		t.Error("system.go: prompt should not be empty")
	}

	// 2. decompose.go - task decomposition
	if DecomposePrompt() == "" {
		t.Error("decompose.go: prompt should not be empty")
	}

	// 3. observe.go - chunked observation
	if ObservePrompt() == "" {
		t.Error("observe.go: prompt should not be empty")
	}

	// 4. forms.go - form filling
	if FormFillingPrompt() == "" {
		t.Error("forms.go: prompt should not be empty")
	}

	// 5. download.go - file download
	if FileDownloadPrompt() == "" {
		t.Error("download.go: prompt should not be empty")
	}

	// 6. summarize.go - task summarization
	if SummarizePrompt() == "" {
		t.Error("summarize.go: prompt should not be empty")
	}

	// Verify all templates have correct types
	if SystemPromptTemplate().Type != PromptSystem {
		t.Error("system template type mismatch")
	}
	if DecomposePromptTemplate().Type != PromptTaskDecomposition {
		t.Error("decompose template type mismatch")
	}
	if ObservePromptTemplate().Type != PromptObserveReasoning {
		t.Error("observe template type mismatch")
	}
	if FormFillingPromptTemplate().Type != PromptFormFilling {
		t.Error("forms template type mismatch")
	}
	if FileDownloadPromptTemplate().Type != PromptFileDownload {
		t.Error("download template type mismatch")
	}
	if SummarizePromptTemplate().Type != PromptSummarizeTask {
		t.Error("summarize template type mismatch")
	}
}
