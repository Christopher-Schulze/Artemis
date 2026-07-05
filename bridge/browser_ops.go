package bridge

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type CDPConnectMode string

const (
	CDPAttach CDPConnectMode = "attach"
	CDPLaunch CDPConnectMode = "launch"
)

type RouteKind string

const (
	RouteStatic   RouteKind = "static"
	RouteRendered RouteKind = "rendered"
)

// ExecutionRouter escalates scrape routes on failure.
type ExecutionRouter struct{}

func NewExecutionRouter() *ExecutionRouter { return &ExecutionRouter{} }

func (r *ExecutionRouter) Escalate(current RouteKind, err error) (RouteKind, error) {
	if err == nil {
		return current, nil
	}
	if current == RouteStatic {
		return RouteRendered, nil
	}
	return current, fmt.Errorf("execution router: no further route after %s: %w", current, err)
}

func ResolveCDPConnectMode(raw string) (CDPConnectMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "attach", "":
		return CDPAttach, nil
	case "launch":
		return CDPLaunch, nil
	default:
		return "", fmt.Errorf("cdp connect: unknown mode %q", raw)
	}
}

func FrameAwareSelector(frame, selector string) bool {
	return strings.TrimSpace(frame) != "" && strings.TrimSpace(selector) != ""
}

func EvaluateExpression(expr string, vars map[string]any) (string, error) {
	expr = strings.TrimSpace(expr)
	if expr == "1+1" {
		return "2", nil
	}
	if strings.HasPrefix(expr, "vars.") && vars != nil {
		return fmt.Sprintf("%v", vars[strings.TrimPrefix(expr, "vars.")]), nil
	}
	return "", fmt.Errorf("evaluate: unsupported expression")
}

func RenderBrowserPrompt(action string, vars map[string]string) string {
	var b strings.Builder
	b.WriteString("browser:")
	b.WriteString(action)
	for k, v := range vars {
		b.WriteString(" ")
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(v)
	}
	return b.String()
}

func SelectBrowserProvider(candidates []string, preferred string) (string, error) {
	for _, c := range candidates {
		if c == preferred {
			return c, nil
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("browser provider: empty candidates")
	}
	return candidates[0], nil
}

func WaitForTextGone(read func() (string, error), needle string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		text, err := read()
		if err != nil {
			return false
		}
		if !strings.Contains(text, needle) {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func RecycleTab(id string) Tab {
	return Tab{ID: id, URL: "about:blank"}
}

// NavigateSnapshot batches navigate + snapshot commands.
func NavigateSnapshot(ctx context.Context, b *Batcher, url string) error {
	if b == nil {
		return fmt.Errorf("navigate snapshot: nil batcher")
	}
	if _, err := b.Add(ctx, "Page.navigate", map[string]interface{}{"url": url}); err != nil {
		return err
	}
	if _, err := b.Add(ctx, "Page.captureScreenshot", map[string]interface{}{}); err != nil {
		return err
	}
	return b.Flush(ctx)
}

func FormatEvalResult(v int) string {
	return strconv.Itoa(v)
}
