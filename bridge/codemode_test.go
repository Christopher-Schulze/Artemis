package bridge

import (
	"context"
	"strings"
	"testing"
)

func TestNormalizeCodeBareExpression(t *testing.T) {
	result := NormalizeCode("42")
	if !strings.Contains(result, "return 42") {
		t.Fatalf("expected return wrapper, got %s", result)
	}
}

func TestNormalizeCodeAsyncFunction(t *testing.T) {
	code := "async () => { return 1; }"
	result := NormalizeCode(code)
	if result != code {
		t.Fatalf("expected unchanged async, got %s", result)
	}
}

func TestNormalizeCodeIIFE(t *testing.T) {
	code := "(async () => { return 1; })()"
	result := NormalizeCode(code)
	if result != code {
		t.Fatalf("expected unchanged IIFE, got %s", result)
	}
}

func TestNormalizeCodeReturnStatement(t *testing.T) {
	code := "return 42;"
	result := NormalizeCode(code)
	if result != code {
		t.Fatalf("expected unchanged return, got %s", result)
	}
}

func TestNormalizeCodeConstDeclaration(t *testing.T) {
	code := "const x = await browser.click({ref: 'e5'});"
	result := NormalizeCode(code)
	if result != code {
		t.Fatalf("expected unchanged const, got %s", result)
	}
}

func TestNormalizeCodeMarkdownFence(t *testing.T) {
	code := "```js\nreturn 42;\n```"
	result := NormalizeCode(code)
	if !strings.Contains(result, "return 42") {
		t.Fatalf("expected fence stripped, got %s", result)
	}
	if strings.Contains(result, "```") {
		t.Fatalf("expected no fences, got %s", result)
	}
}

func TestNormalizeCodeMarkdownFenceNoLang(t *testing.T) {
	code := "```\nreturn 42;\n```"
	result := NormalizeCode(code)
	if !strings.Contains(result, "return 42") {
		t.Fatalf("expected fence stripped, got %s", result)
	}
}

func TestNormalizeCodeEmpty(t *testing.T) {
	result := NormalizeCode("")
	if result != "" {
		t.Fatalf("expected empty, got %s", result)
	}
}

func TestNormalizeCodeIfStatement(t *testing.T) {
	code := "if (true) { return 1; }"
	result := NormalizeCode(code)
	if result != code {
		t.Fatalf("expected unchanged if, got %s", result)
	}
}

func TestCodeModeExecutorExecute(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	executor := NewCodeModeExecutor(session)
	executor.RegisterProvider(DefaultBrowserProvider())

	result, err := executor.Execute(context.Background(), "return 42;")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
}

func TestCodeModeExecutorEmptyCode(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	executor := NewCodeModeExecutor(session)
	result, err := executor.Execute(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Fatal("expected failure for empty code")
	}
}

func TestCodeModeExecutorNoSession(t *testing.T) {
	executor := NewCodeModeExecutor(nil)
	_, err := executor.Execute(context.Background(), "return 1;")
	if err == nil {
		t.Fatal("expected error for nil session")
	}
}

func TestCodeModeExecutorRegisterProvider(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	executor := NewCodeModeExecutor(session)
	executor.RegisterProvider(DefaultBrowserProvider())
	if executor.providers["browser"] == nil {
		t.Fatal("expected browser provider registered")
	}
}

func TestCodeModeExecutorSetTimeout(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	executor := NewCodeModeExecutor(session)
	executor.SetTimeout(5000_000_000) // 5s in ns
	// Just verify no panic
}

func TestCodeModeExecutorGenerateTypeDeclarations(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	executor := NewCodeModeExecutor(session)
	executor.RegisterProvider(DefaultBrowserProvider())
	decls := executor.GenerateTypeDeclarations()
	if !strings.Contains(decls, "browser") {
		t.Fatal("expected browser in type declarations")
	}
	if !strings.Contains(decls, "click") {
		t.Fatal("expected click in type declarations")
	}
	if !strings.Contains(decls, "type") {
		t.Fatal("expected type in type declarations")
	}
}

func TestDefaultBrowserProvider(t *testing.T) {
	provider := DefaultBrowserProvider()
	if provider.Name != "browser" {
		t.Fatalf("expected name 'browser', got %s", provider.Name)
	}
	expectedTools := []string{"click", "type", "getText", "screenshot", "navigate", "snapshot"}
	for _, tool := range expectedTools {
		if provider.Tools[tool] == nil {
			t.Errorf("expected tool %s", tool)
		}
	}
}

func TestDefaultBrowserProviderClick(t *testing.T) {
	provider := DefaultBrowserProvider()
	result, err := provider.Tools["click"](context.Background(), map[string]interface{}{"ref": "e5"})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected map result")
	}
	if m["clicked"] != "e5" {
		t.Fatalf("expected clicked e5, got %v", m["clicked"])
	}
}

func TestDefaultBrowserProviderClickNoRef(t *testing.T) {
	provider := DefaultBrowserProvider()
	_, err := provider.Tools["click"](context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing ref")
	}
}

func TestDefaultBrowserProviderType(t *testing.T) {
	provider := DefaultBrowserProvider()
	result, err := provider.Tools["type"](context.Background(), map[string]interface{}{"ref": "e3", "text": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected map result")
	}
	if m["text"] != "hello" {
		t.Fatalf("expected text hello, got %v", m["text"])
	}
}

func TestDefaultBrowserProviderNavigate(t *testing.T) {
	provider := DefaultBrowserProvider()
	result, err := provider.Tools["navigate"](context.Background(), map[string]interface{}{"url": "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected map result")
	}
	if m["navigated"] != "https://example.com" {
		t.Fatalf("expected navigated url, got %v", m["navigated"])
	}
}

func TestDefaultBrowserProviderNavigateNoUrl(t *testing.T) {
	provider := DefaultBrowserProvider()
	_, err := provider.Tools["navigate"](context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing url")
	}
}

func TestCodeModeExecutorWrapWithProviders(t *testing.T) {
	session := &BrowserSession{ProviderName: "test", SessionID: "s1"}
	executor := NewCodeModeExecutor(session)
	executor.RegisterProvider(DefaultBrowserProvider())
	result, err := executor.Execute(context.Background(), "return await browser.click({ref: 'e5'});")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.Error)
	}
}
