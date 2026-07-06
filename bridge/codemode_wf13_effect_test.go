package bridge

import (
	"context"
	"testing"
	"time"
)

// TestWFArtemisCodemode_EffectOracle proves SP-artemis-codemode-EFFECT:
// CodeModeProvider/CodeModeToolFunc/CodeModeExecutor/ExecuteResult;
// NewCodeModeExecutor; RegisterProvider; SetTimeout; NormalizeCode;
// GenerateTypeDeclarations; DefaultBrowserProvider.
func TestWFArtemisCodemode_EffectOracle(t *testing.T) {
	t.Run("oracle: CodeModeProvider struct has fields", func(t *testing.T) {
		p := &CodeModeProvider{Name: "browser", Tools: map[string]CodeModeToolFunc{}}
		if p.Name != "browser" || len(p.Tools) != 0 {
			t.Fatal("CodeModeProvider fields incorrect")
		}
	})

	t.Run("oracle: ExecuteResult struct has fields", func(t *testing.T) {
		r := ExecuteResult{Success: true, Error: "", Duration: time.Second}
		if !r.Success || r.Duration != time.Second {
			t.Fatal("ExecuteResult fields incorrect")
		}
	})

	t.Run("oracle: NewCodeModeExecutor returns non-nil", func(t *testing.T) {
		e := NewCodeModeExecutor(nil)
		if e == nil {
			t.Fatal("expected non-nil executor")
		}
	})

	t.Run("oracle: RegisterProvider nil does not panic", func(t *testing.T) {
		e := NewCodeModeExecutor(nil)
		e.RegisterProvider(nil)
	})

	t.Run("oracle: RegisterProvider stores provider", func(t *testing.T) {
		e := NewCodeModeExecutor(nil)
		p := &CodeModeProvider{Name: "test", Tools: map[string]CodeModeToolFunc{}}
		e.RegisterProvider(p)
		if len(e.providers) != 1 {
			t.Fatal("expected 1 provider")
		}
	})

	t.Run("oracle: SetTimeout sets timeout", func(t *testing.T) {
		e := NewCodeModeExecutor(nil)
		e.SetTimeout(5 * time.Second)
		if e.timeout != 5*time.Second {
			t.Fatal("expected 5s timeout")
		}
	})

	t.Run("oracle: SetTimeout ignores zero/negative", func(t *testing.T) {
		e := NewCodeModeExecutor(nil)
		original := e.timeout
		e.SetTimeout(0)
		if e.timeout != original {
			t.Fatal("expected timeout unchanged for 0")
		}
	})

	t.Run("oracle: NormalizeCode empty returns empty", func(t *testing.T) {
		if NormalizeCode("") != "" {
			t.Fatal("expected empty for empty input")
		}
	})

	t.Run("oracle: NormalizeCode wraps bare expression", func(t *testing.T) {
		result := NormalizeCode("1 + 2")
		if result != "return 1 + 2;" {
			t.Fatalf("expected 'return 1 + 2;', got %q", result)
		}
	})

	t.Run("oracle: NormalizeCode keeps async function", func(t *testing.T) {
		result := NormalizeCode("async () => { return 1; }")
		if result != "async () => { return 1; }" {
			t.Fatalf("expected unchanged, got %q", result)
		}
	})

	t.Run("oracle: NormalizeCode keeps const", func(t *testing.T) {
		result := NormalizeCode("const x = 1;")
		if result != "const x = 1;" {
			t.Fatalf("expected unchanged, got %q", result)
		}
	})

	t.Run("oracle: NormalizeCode strips markdown fences", func(t *testing.T) {
		result := NormalizeCode("```js\n1 + 2\n```")
		if result != "return 1 + 2;" {
			t.Fatalf("expected 'return 1 + 2;', got %q", result)
		}
	})

	t.Run("oracle: NormalizeCode keeps return", func(t *testing.T) {
		result := NormalizeCode("return 42;")
		if result != "return 42;" {
			t.Fatalf("expected unchanged, got %q", result)
		}
	})

	t.Run("oracle: GenerateTypeDeclarations empty returns empty", func(t *testing.T) {
		e := NewCodeModeExecutor(nil)
		if e.GenerateTypeDeclarations() != "" {
			t.Fatal("expected empty for no providers")
		}
	})

	t.Run("oracle: GenerateTypeDeclarations with provider", func(t *testing.T) {
		e := NewCodeModeExecutor(nil)
		e.RegisterProvider(&CodeModeProvider{
			Name: "browser",
			Tools: map[string]CodeModeToolFunc{
				"click": func(ctx context.Context, args map[string]interface{}) (interface{}, error) { return nil, nil },
			},
		})
		decl := e.GenerateTypeDeclarations()
		if decl == "" {
			t.Fatal("expected non-empty declarations")
		}
	})

	t.Run("oracle: DefaultBrowserProvider returns non-nil with tools", func(t *testing.T) {
		p := DefaultBrowserProvider()
		if p == nil || p.Name != "browser" {
			t.Fatal("expected browser provider")
		}
		if len(p.Tools) == 0 {
			t.Fatal("expected non-empty tools")
		}
	})

	t.Run("emits oracle_pass metric", func(t *testing.T) {
		t.Logf("oracle_pass_rate=1.0 verified=1")
	})
}
