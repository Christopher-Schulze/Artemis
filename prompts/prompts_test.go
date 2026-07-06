package prompts

import (
	"strings"
	"testing"
)

func TestAllPromptTypes(t *testing.T) {
	types := AllPromptTypes()
	if len(types) != 9 {
		t.Fatalf("expected 9 prompt types, got %d", len(types))
	}
}

func TestAllTemplates(t *testing.T) {
	templates := AllTemplates()
	if len(templates) != 9 {
		t.Fatalf("expected 9 templates, got %d", len(templates))
	}
	for _, tmpl := range templates {
		if tmpl.Template == "" {
			t.Fatalf("template %s is empty", tmpl.Name)
		}
		if tmpl.Name == "" {
			t.Fatal("template name is empty")
		}
	}
}

func TestGetPrompt(t *testing.T) {
	tmpl, err := GetPrompt(PromptSystem)
	if err != nil {
		t.Fatal(err)
	}
	if tmpl == "" {
		t.Fatal("system prompt is empty")
	}
	if !strings.Contains(tmpl, "one action per iteration") {
		t.Fatal("system prompt must contain 'one action per iteration' rule")
	}
}

func TestGetPromptUnknown(t *testing.T) {
	_, err := GetPrompt(PromptType(99))
	if err == nil {
		t.Fatal("expected error for unknown prompt type")
	}
}

func TestPromptTypeString(t *testing.T) {
	tests := []struct {
		pt   PromptType
		want string
	}{
		{PromptSystem, "system"},
		{PromptTaskDecomposition, "task_decomposition"},
		{PromptDecomposeReflection, "decompose_reflection"},
		{PromptPureReasoning, "pure_reasoning"},
		{PromptObserveReasoning, "observe_reasoning"},
		{PromptSubtaskRevision, "subtask_revision"},
		{PromptFormFilling, "form_filling"},
		{PromptFileDownload, "file_download"},
		{PromptSummarizeTask, "summarize_task"},
	}
	for _, tt := range tests {
		if got := tt.pt.String(); got != tt.want {
			t.Errorf("PromptType(%d).String() = %s, want %s", tt.pt, got, tt.want)
		}
	}
}

func TestGetPromptName(t *testing.T) {
	if GetPromptName(PromptSystem) != "System Prompt" {
		t.Fatal("expected 'System Prompt'")
	}
	if GetPromptName(PromptFormFilling) != "Form Filling" {
		t.Fatal("expected 'Form Filling'")
	}
}

func TestGetPromptTemplate(t *testing.T) {
	pt, err := GetPromptTemplate(PromptFormFilling)
	if err != nil {
		t.Fatal(err)
	}
	if pt.Name != "Form Filling" {
		t.Fatalf("expected 'Form Filling', got %s", pt.Name)
	}
	if !strings.Contains(pt.Template, "DROPDOWN") {
		t.Fatal("form filling prompt must mention DROPDOWN")
	}
	if !strings.Contains(pt.Template, "CRITICAL") {
		t.Fatal("form filling prompt must contain CRITICAL rule")
	}
}

func TestSystemPromptKeyRules(t *testing.T) {
	tmpl, _ := GetPrompt(PromptSystem)
	// Must contain key rules from spec table
	checks := []string{
		"one action per iteration",
		"snapshot",
		"browser_navigate",
		"Do not generate or invent URLs",
	}
	for _, check := range checks {
		if !strings.Contains(strings.ToLower(tmpl), strings.ToLower(check)) {
			t.Errorf("system prompt must contain %q", check)
		}
	}
}

func TestTaskDecompositionKeyRules(t *testing.T) {
	tmpl, _ := GetPrompt(PromptTaskDecomposition)
	if !strings.Contains(tmpl, "Indivisible") {
		t.Fatal("task decomposition must mention Indivisible")
	}
	if !strings.Contains(tmpl, "JSON") {
		t.Fatal("task decomposition must mention JSON output")
	}
}

func TestDecomposeReflectionKeyRules(t *testing.T) {
	tmpl, _ := GetPrompt(PromptDecomposeReflection)
	if !strings.Contains(tmpl, "SUFFICIENT") {
		t.Fatal("decompose reflection must mention SUFFICIENT")
	}
	if !strings.Contains(tmpl, "REVISED_SUBTASKS") {
		t.Fatal("decompose reflection must mention REVISED_SUBTASKS")
	}
}

func TestPureReasoningKeyRules(t *testing.T) {
	tmpl, _ := GetPrompt(PromptPureReasoning)
	if !strings.Contains(tmpl, "reasoning") {
		t.Fatal("pure reasoning must mention reasoning")
	}
}

func TestObserveReasoningKeyRules(t *testing.T) {
	tmpl, _ := GetPrompt(PromptObserveReasoning)
	if !strings.Contains(tmpl, "80KB") {
		t.Fatal("observe reasoning must mention 80KB chunks")
	}
	if !strings.Contains(tmpl, "CONTINUE") {
		t.Fatal("observe reasoning must mention CONTINUE status")
	}
}

func TestSubtaskRevisionKeyRules(t *testing.T) {
	tmpl, _ := GetPrompt(PromptSubtaskRevision)
	if !strings.Contains(tmpl, "revision") {
		t.Fatal("subtask revision must mention revision")
	}
	if !strings.Contains(tmpl, "JSON") {
		t.Fatal("subtask revision must mention JSON array")
	}
}

func TestFormFillingKeyRules(t *testing.T) {
	tmpl, _ := GetPrompt(PromptFormFilling)
	if !strings.Contains(tmpl, "DROPDOWN") {
		t.Fatal("form filling must mention DROPDOWN")
	}
	if !strings.Contains(tmpl, "CHECKBOXES") {
		t.Fatal("form filling must mention CHECKBOXES")
	}
}

func TestFileDownloadKeyRules(t *testing.T) {
	tmpl, _ := GetPrompt(PromptFileDownload)
	if !strings.Contains(tmpl, "download") {
		t.Fatal("file download must mention download")
	}
	if !strings.Contains(tmpl, "PDF") {
		t.Fatal("file download must mention PDF")
	}
}

func TestSummarizeTaskKeyRules(t *testing.T) {
	tmpl, _ := GetPrompt(PromptSummarizeTask)
	if !strings.Contains(tmpl, "NO_ANSWER") {
		t.Fatal("summarize task must mention NO_ANSWER")
	}
	if !strings.Contains(tmpl, "Task Overview") {
		t.Fatal("summarize task must mention Task Overview")
	}
}
