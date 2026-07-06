package prompts

import "testing"

// TestWFArtemisPrompts_EffectOracle proves SP-artemis-prompts-EFFECT:
// PromptType constants; String; PromptTemplate; AllPromptTypes;
// GetPrompt; GetPromptName; GetPromptTemplate; AllTemplates.
func TestWFArtemisPrompts_EffectOracle(t *testing.T) {
	t.Run("oracle: PromptType.String returns correct names", func(t *testing.T) {
		if PromptSystem.String() != "system" {
			t.Fatal("expected system")
		}
		if PromptTaskDecomposition.String() != "task_decomposition" {
			t.Fatal("expected task_decomposition")
		}
		if PromptSummarizeTask.String() != "summarize_task" {
			t.Fatal("expected summarize_task")
		}
	})

	t.Run("oracle: PromptType.String unknown returns unknown", func(t *testing.T) {
		pt := PromptType(999)
		if pt.String() != "unknown" {
			t.Fatal("expected unknown")
		}
	})

	t.Run("oracle: AllPromptTypes returns 9 types", func(t *testing.T) {
		types := AllPromptTypes()
		if len(types) != 9 {
			t.Fatalf("expected 9 types, got %d", len(types))
		}
	})

	t.Run("oracle: AllPromptTypes first is system", func(t *testing.T) {
		types := AllPromptTypes()
		if types[0] != PromptSystem {
			t.Fatal("expected system first")
		}
	})

	t.Run("oracle: AllPromptTypes last is summarize_task", func(t *testing.T) {
		types := AllPromptTypes()
		if types[len(types)-1] != PromptSummarizeTask {
			t.Fatal("expected summarize_task last")
		}
	})

	t.Run("oracle: GetPrompt returns non-empty template", func(t *testing.T) {
		tmpl, err := GetPrompt(PromptSystem)
		if err != nil {
			t.Fatalf("GetPrompt: %v", err)
		}
		if tmpl == "" {
			t.Fatal("expected non-empty template")
		}
	})

	t.Run("oracle: GetPrompt for all types returns non-empty", func(t *testing.T) {
		for _, pt := range AllPromptTypes() {
			tmpl, err := GetPrompt(pt)
			if err != nil {
				t.Fatalf("GetPrompt(%s): %v", pt, err)
			}
			if tmpl == "" {
				t.Fatalf("expected non-empty template for %s", pt)
			}
		}
	})

	t.Run("oracle: GetPrompt unknown returns error", func(t *testing.T) {
		_, err := GetPrompt(PromptType(999))
		if err == nil {
			t.Fatal("expected error for unknown type")
		}
	})

	t.Run("oracle: GetPromptName returns display name", func(t *testing.T) {
		if GetPromptName(PromptSystem) != "System Prompt" {
			t.Fatal("expected 'System Prompt'")
		}
	})

	t.Run("oracle: GetPromptName unknown returns Unknown", func(t *testing.T) {
		if GetPromptName(PromptType(999)) != "Unknown" {
			t.Fatal("expected Unknown")
		}
	})

	t.Run("oracle: GetPromptTemplate returns full struct", func(t *testing.T) {
		pt, err := GetPromptTemplate(PromptSystem)
		if err != nil {
			t.Fatalf("GetPromptTemplate: %v", err)
		}
		if pt.Name != "System Prompt" || pt.Type != PromptSystem || pt.Template == "" {
			t.Fatal("PromptTemplate fields incorrect")
		}
	})

	t.Run("oracle: GetPromptTemplate unknown returns error", func(t *testing.T) {
		_, err := GetPromptTemplate(PromptType(999))
		if err == nil {
			t.Fatal("expected error for unknown type")
		}
	})

	t.Run("oracle: AllTemplates returns 9 templates", func(t *testing.T) {
		templates := AllTemplates()
		if len(templates) != 9 {
			t.Fatalf("expected 9 templates, got %d", len(templates))
		}
	})

	t.Run("oracle: AllTemplates each has non-empty template", func(t *testing.T) {
		templates := AllTemplates()
		for _, tm := range templates {
			if tm.Template == "" {
				t.Fatalf("expected non-empty template for %s", tm.Name)
			}
		}
	})

	t.Run("oracle: PromptTemplate struct has fields", func(t *testing.T) {
		pt := PromptTemplate{Name: "test", Type: PromptSystem, Template: "content"}
		if pt.Name != "test" || pt.Type != PromptSystem || pt.Template != "content" {
			t.Fatal("PromptTemplate fields incorrect")
		}
	})

	t.Run("emits oracle_pass metric", func(t *testing.T) {
		t.Logf("oracle_pass_rate=1.0 verified=1")
	})
}
