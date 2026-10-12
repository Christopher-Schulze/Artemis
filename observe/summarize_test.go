package observe

import (
	"strings"
	"sync"
	"testing"
)

func TestDefaultSummarizeConfig_Threshold8000(t *testing.T) {
	cfg := DefaultSummarizeConfig()
	if cfg.Threshold != 8000 {
		t.Fatalf("expected threshold=8000, got %d", cfg.Threshold)
	}
	if cfg.MaxTokens <= 0 {
		t.Fatalf("expected positive max tokens, got %d", cfg.MaxTokens)
	}
	if cfg.FallbackTruncate <= 0 {
		t.Fatalf("expected positive fallback truncate, got %d", cfg.FallbackTruncate)
	}
}

func TestDefaultSummarizeThresholdConstant(t *testing.T) {
	if DefaultSummarizeThreshold != 8000 {
		t.Fatalf("expected DefaultSummarizeThreshold=8000, got %d", DefaultSummarizeThreshold)
	}
}

func TestNewSummarizer_AppliesDefaultsForZero(t *testing.T) {
	s := NewSummarizer(SummarizeConfig{})
	cfg := s.Config()
	if cfg.Threshold != DefaultSummarizeThreshold {
		t.Fatalf("expected default threshold, got %d", cfg.Threshold)
	}
	if cfg.MaxTokens != DefaultMaxTokens {
		t.Fatalf("expected default max tokens, got %d", cfg.MaxTokens)
	}
	if cfg.FallbackTruncate != DefaultFallbackTruncateLen {
		t.Fatalf("expected default fallback, got %d", cfg.FallbackTruncate)
	}
}

func TestNewSummarizer_PreservesCustomConfig(t *testing.T) {
	s := NewSummarizer(SummarizeConfig{Threshold: 1000, MaxTokens: 500, FallbackTruncate: 200})
	cfg := s.Config()
	if cfg.Threshold != 1000 {
		t.Fatalf("expected threshold=1000, got %d", cfg.Threshold)
	}
	if cfg.MaxTokens != 500 {
		t.Fatalf("expected max tokens=500, got %d", cfg.MaxTokens)
	}
	if cfg.FallbackTruncate != 200 {
		t.Fatalf("expected fallback=200, got %d", cfg.FallbackTruncate)
	}
}

func TestNewDefaultSummarizer(t *testing.T) {
	s := NewDefaultSummarizer()
	cfg := s.Config()
	if cfg.Threshold != 8000 {
		t.Fatalf("expected threshold=8000, got %d", cfg.Threshold)
	}
}

func TestSummarizer_SetConfig(t *testing.T) {
	s := NewDefaultSummarizer()
	s.SetConfig(SummarizeConfig{Threshold: 500})
	if s.Config().Threshold != 500 {
		t.Fatalf("expected threshold=500, got %d", s.Config().Threshold)
	}
	// Zero values reset to defaults.
	s.SetConfig(SummarizeConfig{})
	if s.Config().Threshold != DefaultSummarizeThreshold {
		t.Fatalf("expected reset to default, got %d", s.Config().Threshold)
	}
}

func TestSummarizer_NeedsSummarization_BelowThreshold(t *testing.T) {
	s := NewDefaultSummarizer()
	if s.NeedsSummarization("short content") {
		t.Error("expected no summarization needed for short content")
	}
}

func TestSummarizer_NeedsSummarization_AboveThreshold(t *testing.T) {
	s := NewDefaultSummarizer()
	long := strings.Repeat("a", 8001)
	if !s.NeedsSummarization(long) {
		t.Error("expected summarization needed for >8000 chars")
	}
}

func TestSummarizer_NeedsSummarization_ExactThreshold(t *testing.T) {
	s := NewDefaultSummarizer()
	exact := strings.Repeat("a", 8000)
	if s.NeedsSummarization(exact) {
		t.Error("expected no summarization at exactly 8000 chars (strictly greater)")
	}
}

func TestSummarizer_Summarize_ShortContentNoSummary(t *testing.T) {
	s := NewDefaultSummarizer()
	content := "This is a short snapshot."
	out, wasSummarized := s.Summarize(content, "find login button")
	if wasSummarized {
		t.Error("expected wasSummarized=false for short content")
	}
	if out != content {
		t.Errorf("expected unchanged content, got %q", out)
	}
}

func TestSummarizer_Summarize_LongContentWithRelevant(t *testing.T) {
	s := NewDefaultSummarizer()
	// Build content >8000 chars with a task-relevant sentence.
	relevant := "The login button is located at the top right of the page."
	irrelevant := strings.Repeat("This is filler content about weather. ", 250) // ~10000 chars
	content := irrelevant + " " + relevant + " " + irrelevant
	if len(content) <= 8000 {
		t.Fatalf("test content too short: %d", len(content))
	}
	out, wasSummarized := s.Summarize(content, "find the login button")
	if !wasSummarized {
		t.Fatal("expected wasSummarized=true")
	}
	if !strings.Contains(out, "login button") {
		t.Errorf("expected summary to contain relevant sentence, got %q", out)
	}
	if len(out) >= len(content) {
		t.Errorf("expected summary shorter than original: %d vs %d", len(out), len(content))
	}
}

func TestSummarizer_Summarize_LongContentFallbackTruncation(t *testing.T) {
	s := NewDefaultSummarizer()
	// Content with no task-relevant keywords.
	content := strings.Repeat("aaaa ", 2000) // ~10000 chars
	out, wasSummarized := s.Summarize(content, "zzzzz")
	if !wasSummarized {
		t.Fatal("expected wasSummarized=true for long content")
	}
	if !strings.HasSuffix(out, "...") {
		t.Errorf("expected truncated suffix '...', got %q", out)
	}
	if len(out) > DefaultFallbackTruncateLen {
		t.Errorf("expected len <= %d, got %d", DefaultFallbackTruncateLen, len(out))
	}
}

func TestSummarizer_Summarize_EmptyTaskFallback(t *testing.T) {
	s := NewDefaultSummarizer()
	content := strings.Repeat("abcdefghij", 1000) // 10000 chars
	out, wasSummarized := s.Summarize(content, "")
	if !wasSummarized {
		t.Fatal("expected wasSummarized=true for long content with empty task")
	}
	if !strings.HasSuffix(out, "...") {
		t.Errorf("expected fallback truncation suffix, got %q", out)
	}
}

func TestSummarizer_SummarizeResult_Metadata(t *testing.T) {
	s := NewDefaultSummarizer()
	content := strings.Repeat("x", 9000)
	result := s.SummarizeResult(content, "test task")
	if result.OriginalLength != 9000 {
		t.Errorf("expected original length=9000, got %d", result.OriginalLength)
	}
	if !result.WasSummarized {
		t.Error("expected WasSummarized=true")
	}
	if result.Task != "test task" {
		t.Errorf("expected task='test task', got %q", result.Task)
	}
	if result.SummarizedLength <= 0 {
		t.Errorf("expected positive summarized length, got %d", result.SummarizedLength)
	}
}

func TestSummarizer_Stats_Initial(t *testing.T) {
	s := NewDefaultSummarizer()
	stats := s.Stats()
	if stats.Total != 0 || stats.Summarized != 0 || stats.Truncated != 0 {
		t.Fatalf("expected zero stats, got %+v", stats)
	}
}

func TestSummarizer_Stats_AfterSummarize(t *testing.T) {
	s := NewDefaultSummarizer()
	// Short call: counts as Total, not Summarized.
	s.Summarize("short", "task")
	// Long call with relevant content.
	relevant := "The login button is here."
	content := strings.Repeat("filler content. ", 600) + relevant
	s.Summarize(content, "login button")
	// Long call with no relevant content (truncation fallback).
	s.Summarize(strings.Repeat("z", 9000), "nomatch")

	stats := s.Stats()
	if stats.Total != 3 {
		t.Fatalf("expected total=3, got %d", stats.Total)
	}
	if stats.Summarized != 2 {
		t.Fatalf("expected summarized=2, got %d", stats.Summarized)
	}
	if stats.Truncated != 1 {
		t.Fatalf("expected truncated=1, got %d", stats.Truncated)
	}
}

func TestTruncateContent_NoTruncationNeeded(t *testing.T) {
	out := TruncateContent("short", 100)
	if out != "short" {
		t.Errorf("expected unchanged, got %q", out)
	}
}

func TestTruncateContent_TruncatesWithSuffix(t *testing.T) {
	out := TruncateContent("abcdefghij", 7)
	if out != "abcd..." {
		t.Errorf("expected 'abcd...', got %q", out)
	}
}

func TestTruncateContent_ExactLength(t *testing.T) {
	out := TruncateContent("abcdef", 6)
	if out != "abcdef" {
		t.Errorf("expected unchanged at exact length, got %q", out)
	}
}

func TestTruncateContent_ZeroMaxLen(t *testing.T) {
	out := TruncateContent("anything", 0)
	if out != "anything" {
		t.Errorf("expected unchanged for maxLen=0, got %q", out)
	}
}

func TestTruncateContent_TinyMaxLen(t *testing.T) {
	out := TruncateContent("abcdef", 2)
	if len(out) != 2 {
		t.Fatalf("expected len=2, got %d", len(out))
	}
}

func TestSummarizer_ExtractRelevant_MatchesKeywords(t *testing.T) {
	s := NewDefaultSummarizer()
	content := "The login button is red. The weather is nice. Click the login button to proceed."
	out := s.ExtractRelevant(content, "login button")
	if !strings.Contains(out, "login button is red") {
		t.Errorf("expected first matching sentence, got %q", out)
	}
	if !strings.Contains(out, "login button to proceed") {
		t.Errorf("expected second matching sentence, got %q", out)
	}
	if strings.Contains(out, "weather") {
		t.Errorf("expected irrelevant sentence excluded, got %q", out)
	}
}

func TestSummarizer_ExtractRelevant_NoMatch(t *testing.T) {
	s := NewDefaultSummarizer()
	out := s.ExtractRelevant("nothing relevant here.", "zzzzz")
	if out != "" {
		t.Errorf("expected empty for no match, got %q", out)
	}
}

func TestSummarizer_ExtractRelevant_EmptyTask(t *testing.T) {
	s := NewDefaultSummarizer()
	out := s.ExtractRelevant("some content", "")
	if out != "" {
		t.Errorf("expected empty for empty task, got %q", out)
	}
}

func TestSummarizer_ExtractRelevant_CaseInsensitive(t *testing.T) {
	s := NewDefaultSummarizer()
	content := "The LOGIN button is here. Other text."
	out := s.ExtractRelevant(content, "login")
	if !strings.Contains(out, "LOGIN") {
		t.Errorf("expected case-insensitive match, got %q", out)
	}
}

func TestExtractKeywords_DropsStopwords(t *testing.T) {
	kw := extractKeywords("find the login button on the page")
	joined := strings.Join(kw, ",")
	if strings.Contains(joined, "the") {
		t.Errorf("expected stopwords dropped, got %s", joined)
	}
	if !strings.Contains(joined, "login") {
		t.Errorf("expected 'login' keyword, got %s", joined)
	}
}

func TestExtractKeywords_EmptyTask(t *testing.T) {
	if kw := extractKeywords(""); kw != nil {
		t.Errorf("expected nil for empty task, got %v", kw)
	}
}

func TestSplitSentences_Basic(t *testing.T) {
	sentences := splitSentences("First sentence. Second one! Third? Done.")
	if len(sentences) != 4 {
		t.Fatalf("expected 4 sentences, got %d: %v", len(sentences), sentences)
	}
}

func TestSplitSentences_NewlineBoundary(t *testing.T) {
	sentences := splitSentences("line one\nline two\nline three")
	if len(sentences) != 3 {
		t.Fatalf("expected 3 sentences, got %d", len(sentences))
	}
}

func TestSplitSentences_Empty(t *testing.T) {
	if sentences := splitSentences(""); sentences != nil {
		t.Errorf("expected nil for empty, got %v", sentences)
	}
}

func TestSummarizer_Summarize_RelevantCappedToTokenBudget(t *testing.T) {
	s := NewSummarizer(SummarizeConfig{Threshold: 100, MaxTokens: 10, FallbackTruncate: 50})
	// Many relevant sentences; should be capped to ~40 chars (10 tokens * 4).
	relevant := strings.Repeat("login button here. ", 200)
	content := strings.Repeat("filler ", 50) + relevant
	out, wasSummarized := s.Summarize(content, "login")
	if !wasSummarized {
		t.Fatal("expected wasSummarized=true")
	}
	if len(out) > 40+3 { // cap + suffix
		t.Errorf("expected output capped near 40 chars, got %d: %q", len(out), out)
	}
}

func TestSummarizer_ConcurrentSummarize(t *testing.T) {
	s := NewSummarizer(SummarizeConfig{Threshold: 100, MaxTokens: 100, FallbackTruncate: 50})
	content := strings.Repeat("a", 500)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, wasSummarized := s.Summarize(content, "test")
			if !wasSummarized {
				t.Error("expected wasSummarized=true")
			}
			if len(out) == 0 {
				t.Error("expected non-empty output")
			}
		}()
	}
	wg.Wait()
	stats := s.Stats()
	if stats.Total != 20 {
		t.Fatalf("expected total=20, got %d", stats.Total)
	}
}
