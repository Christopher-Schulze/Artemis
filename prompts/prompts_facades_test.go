package prompts

import (
	"strings"
	"testing"
)

// ==================== system.go tests ====================

// TestTASK2251_SystemPrompt verifies system prompt content
// (spec L4029: system.go - base behavior).
func TestTASK2251_SystemPrompt(t *testing.T) {
	p := SystemPrompt()
	if p == "" {
		t.Error("system prompt should not be empty")
	}
	if !strings.Contains(p, "Web Using AI") {
		t.Error("system prompt should contain 'Web Using AI'")
	}
}

// TestTASK2251_SystemPromptType verifies prompt type
// (spec L4029: system.go).
func TestTASK2251_SystemPromptType(t *testing.T) {
	if SystemPromptType() != PromptSystem {
		t.Error("system prompt type should be PromptSystem")
	}
}

// TestTASK2251_SystemPromptTemplate verifies template
// (spec L4029: system.go - base behavior).
func TestTASK2251_SystemPromptTemplate(t *testing.T) {
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

// TestTASK2251_DecomposePrompt verifies decomposition prompt
// (spec L4029: decompose.go - task decomposition).
func TestTASK2251_DecomposePrompt(t *testing.T) {
	p := DecomposePrompt()
	if p == "" {
		t.Error("decompose prompt should not be empty")
	}
	if !strings.Contains(p, "Decomposition") {
		t.Error("decompose prompt should contain 'Decomposition'")
	}
}

// TestTASK2251_DecomposePromptType verifies prompt type
// (spec L4029: decompose.go).
func TestTASK2251_DecomposePromptType(t *testing.T) {
	if DecomposePromptType() != PromptTaskDecomposition {
		t.Error("decompose prompt type should be PromptTaskDecomposition")
	}
}

// TestTASK2251_DecomposePromptTemplate verifies template
// (spec L4029: decompose.go - task decomposition).
func TestTASK2251_DecomposePromptTemplate(t *testing.T) {
	tmpl := DecomposePromptTemplate()
	if tmpl.Name != "task_decomposition" {
		t.Errorf("name: got %s, want task_decomposition", tmpl.Name)
	}
	if tmpl.Type != PromptTaskDecomposition {
		t.Error("type should be PromptTaskDecomposition")
	}
}

// TestTASK2251_DecomposeReflectionPrompt verifies reflection prompt
// (spec L4029: decompose.go - task decomposition).
func TestTASK2251_DecomposeReflectionPrompt(t *testing.T) {
	p := DecomposeReflectionPrompt()
	if p == "" {
		t.Error("reflection prompt should not be empty")
	}
}

// TestTASK2251_DecomposeReflectionPromptType verifies prompt type.
func TestTASK2251_DecomposeReflectionPromptType(t *testing.T) {
	if DecomposeReflectionPromptType() != PromptDecomposeReflection {
		t.Error("reflection type should be PromptDecomposeReflection")
	}
}

// TestTASK2251_DecomposeReflectionPromptTemplate verifies template.
func TestTASK2251_DecomposeReflectionPromptTemplate(t *testing.T) {
	tmpl := DecomposeReflectionPromptTemplate()
	if tmpl.Type != PromptDecomposeReflection {
		t.Error("type should be PromptDecomposeReflection")
	}
}

// ==================== observe.go tests ====================

// TestTASK2251_ObservePrompt verifies observation prompt
// (spec L4029: observe.go - chunked observation).
func TestTASK2251_ObservePrompt(t *testing.T) {
	p := ObservePrompt()
	if p == "" {
		t.Error("observe prompt should not be empty")
	}
	if !strings.Contains(p, "snapshot") {
		t.Error("observe prompt should contain 'snapshot'")
	}
}

// TestTASK2251_ObservePromptType verifies prompt type
// (spec L4029: observe.go).
func TestTASK2251_ObservePromptType(t *testing.T) {
	if ObservePromptType() != PromptObserveReasoning {
		t.Error("observe prompt type should be PromptObserveReasoning")
	}
}

// TestTASK2251_ObservePromptTemplate verifies template
// (spec L4029: observe.go - chunked observation).
func TestTASK2251_ObservePromptTemplate(t *testing.T) {
	tmpl := ObservePromptTemplate()
	if tmpl.Name != "observe_reasoning" {
		t.Errorf("name: got %s, want observe_reasoning", tmpl.Name)
	}
	if tmpl.Type != PromptObserveReasoning {
		t.Error("type should be PromptObserveReasoning")
	}
}

// TestTASK2251_PureReasoningPrompt verifies pure reasoning prompt
// (spec L4029: observe.go - chunked observation).
func TestTASK2251_PureReasoningPrompt(t *testing.T) {
	p := PureReasoningPrompt()
	if p == "" {
		t.Error("pure reasoning prompt should not be empty")
	}
}

// TestTASK2251_PureReasoningPromptType verifies prompt type.
func TestTASK2251_PureReasoningPromptType(t *testing.T) {
	if PureReasoningPromptType() != PromptPureReasoning {
		t.Error("pure reasoning type should be PromptPureReasoning")
	}
}

// TestTASK2251_PureReasoningPromptTemplate verifies template.
func TestTASK2251_PureReasoningPromptTemplate(t *testing.T) {
	tmpl := PureReasoningPromptTemplate()
	if tmpl.Type != PromptPureReasoning {
		t.Error("type should be PromptPureReasoning")
	}
}

// ==================== forms.go tests ====================

// TestTASK2251_FormFillingPrompt verifies form filling prompt
// (spec L4029: forms.go - form filling).
func TestTASK2251_FormFillingPrompt(t *testing.T) {
	p := FormFillingPrompt()
	if p == "" {
		t.Error("form filling prompt should not be empty")
	}
	if !strings.Contains(p, "form") {
		t.Error("form filling prompt should contain 'form'")
	}
}

// TestTASK2251_FormFillingPromptType verifies prompt type
// (spec L4029: forms.go).
func TestTASK2251_FormFillingPromptType(t *testing.T) {
	if FormFillingPromptType() != PromptFormFilling {
		t.Error("form filling type should be PromptFormFilling")
	}
}

// TestTASK2251_FormFillingPromptTemplate verifies template
// (spec L4029: forms.go - form filling).
func TestTASK2251_FormFillingPromptTemplate(t *testing.T) {
	tmpl := FormFillingPromptTemplate()
	if tmpl.Name != "form_filling" {
		t.Errorf("name: got %s, want form_filling", tmpl.Name)
	}
	if tmpl.Type != PromptFormFilling {
		t.Error("type should be PromptFormFilling")
	}
}

// ==================== download.go tests ====================

// TestTASK2251_FileDownloadPrompt verifies file download prompt
// (spec L4029: download.go - file download).
func TestTASK2251_FileDownloadPrompt(t *testing.T) {
	p := FileDownloadPrompt()
	if p == "" {
		t.Error("file download prompt should not be empty")
	}
}

// TestTASK2251_FileDownloadPromptType verifies prompt type
// (spec L4029: download.go).
func TestTASK2251_FileDownloadPromptType(t *testing.T) {
	if FileDownloadPromptType() != PromptFileDownload {
		t.Error("file download type should be PromptFileDownload")
	}
}

// TestTASK2251_FileDownloadPromptTemplate verifies template
// (spec L4029: download.go - file download).
func TestTASK2251_FileDownloadPromptTemplate(t *testing.T) {
	tmpl := FileDownloadPromptTemplate()
	if tmpl.Name != "file_download" {
		t.Errorf("name: got %s, want file_download", tmpl.Name)
	}
	if tmpl.Type != PromptFileDownload {
		t.Error("type should be PromptFileDownload")
	}
}

// ==================== summarize.go tests ====================

// TestTASK2251_SummarizePrompt verifies summarize prompt
// (spec L4029: summarize.go - task summarization).
func TestTASK2251_SummarizePrompt(t *testing.T) {
	p := SummarizePrompt()
	if p == "" {
		t.Error("summarize prompt should not be empty")
	}
}

// TestTASK2251_SummarizePromptType verifies prompt type
// (spec L4029: summarize.go).
func TestTASK2251_SummarizePromptType(t *testing.T) {
	if SummarizePromptType() != PromptSummarizeTask {
		t.Error("summarize type should be PromptSummarizeTask")
	}
}

// TestTASK2251_SummarizePromptTemplate verifies template
// (spec L4029: summarize.go - task summarization).
func TestTASK2251_SummarizePromptTemplate(t *testing.T) {
	tmpl := SummarizePromptTemplate()
	if tmpl.Name != "summarize_task" {
		t.Errorf("name: got %s, want summarize_task", tmpl.Name)
	}
	if tmpl.Type != PromptSummarizeTask {
		t.Error("type should be PromptSummarizeTask")
	}
}

// ==================== full spec parity test ====================

// TestTASK2251_FullSpecParity verifies all 6 spec-mandated files
// (spec L4029: system.go, decompose.go, observe.go, forms.go,
// download.go, summarize.go).
func TestTASK2251_FullSpecParity(t *testing.T) {
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
