package scraper

import (
	"context"
	"fmt"
	"time"
)

// RecoveryStep is one stage in the scraper recovery chain (spec recovery.go).
type RecoveryStep string

const (
	StepBackoff      RecoveryStep = "backoff"
	StepRotateProxy  RecoveryStep = "rotate_proxy"
	StepStaticFetch  RecoveryStep = "static_fetch"
	StepEngineRender RecoveryStep = "engine_render"
)

// RecoveryPlan is the ordered recovery chain for a failed scrape.
type RecoveryPlan struct {
	Steps []RecoveryStep
}

// DefaultRecoveryPlan returns the canonical recovery ordering.
func DefaultRecoveryPlan() RecoveryPlan {
	return RecoveryPlan{
		Steps: []RecoveryStep{StepBackoff, StepRotateProxy, StepStaticFetch, StepEngineRender},
	}
}

// RecoveryExecutor runs each step until one succeeds.
type RecoveryExecutor struct {
	Backoff    func(context.Context, int) error
	Rotate     func(context.Context) error
	Static     func(context.Context, string) error
	Render     func(context.Context, string) error
	MaxBackoff int
}

// Run executes the recovery chain for url.
func (e *RecoveryExecutor) Run(ctx context.Context, url string, plan RecoveryPlan) (RecoveryStep, error) {
	if e == nil {
		return "", fmt.Errorf("scraper recovery: nil executor")
	}
	if len(plan.Steps) == 0 {
		plan = DefaultRecoveryPlan()
	}
	var lastErr error
	for _, step := range plan.Steps {
		if err := ctx.Err(); err != nil {
			return step, err
		}
		var err error
		switch step {
		case StepBackoff:
			max := e.MaxBackoff
			if max <= 0 {
				max = 3
			}
			if e.Backoff == nil {
				err = fmt.Errorf("backoff handler missing")
			} else {
				err = e.Backoff(ctx, max)
			}
		case StepRotateProxy:
			if e.Rotate == nil {
				err = fmt.Errorf("rotate handler missing")
			} else {
				err = e.Rotate(ctx)
			}
		case StepStaticFetch:
			if e.Static == nil {
				err = fmt.Errorf("static handler missing")
			} else {
				err = e.Static(ctx, url)
			}
		case StepEngineRender:
			if e.Render == nil {
				err = fmt.Errorf("render handler missing")
			} else {
				err = e.Render(ctx, url)
			}
		default:
			err = fmt.Errorf("unknown recovery step %q", step)
		}
		if err == nil {
			return step, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("recovery chain exhausted")
	}
	return "", lastErr
}

// BackoffDelay returns exponential delay for attempt n (base=200ms).
func BackoffDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	ms := 200 * (1 << attempt)
	if ms > 5000 {
		ms = 5000
	}
	return time.Duration(ms) * time.Millisecond
}
