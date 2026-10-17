package actions

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge/cdpops"
	"github.com/Christopher-Schulze/Artemis/input"
	"github.com/Christopher-Schulze/Artemis/internal/random"
)

// HumanProfile requests human-like click, type and scroll input executed with
// real CDP events (spec L4190-L4194). Nil keeps the fast canonical path. A
// non-zero Seed makes generated paths, offsets and timings reproducible; zero
// uses the cryptographic source.
type HumanProfile struct {
	Seed int64 `json:"seed,omitempty"`
	// MovePath replaces the generated Bezier approach; its last point is the
	// press position.
	MovePath      []input.MousePoint `json:"movePath,omitempty"`
	PreClickDelay time.Duration      `json:"preClickDelay,omitempty"`
	// KeyDelay with KeyVariance replaces the character-aware keystroke rhythm.
	KeyDelay    time.Duration `json:"keyDelay,omitempty"`
	KeyVariance time.Duration `json:"keyVariance,omitempty"`
	// TypoRate inserts corrected typos (wrong key, Backspace, right key).
	TypoRate       float64       `json:"typoRate,omitempty"`
	ScrollSteps    int           `json:"scrollSteps,omitempty"`
	ScrollDuration time.Duration `json:"scrollDuration,omitempty"`
}

const (
	maxHumanMovePoints     = 512
	maxHumanPreClickDelay  = 5 * time.Second
	maxHumanKeyDelay       = 2 * time.Second
	maxHumanTypoRate       = 0.2
	maxHumanScrollSteps    = 200
	maxHumanScrollDuration = 10 * time.Second
	defaultHumanScrollStep = 20
	defaultHumanScrollTime = 500 * time.Millisecond
)

func (p *HumanProfile) validate() error {
	if p == nil {
		return nil
	}
	switch {
	case len(p.MovePath) > maxHumanMovePoints:
		return fmt.Errorf("human input: move path exceeds %d points", maxHumanMovePoints)
	case p.PreClickDelay < 0 || p.PreClickDelay > maxHumanPreClickDelay:
		return errors.New("human input: pre-click delay out of bounds")
	case p.KeyDelay < 0 || p.KeyDelay > maxHumanKeyDelay || p.KeyVariance < 0 || p.KeyVariance > p.KeyDelay:
		return errors.New("human input: keystroke delay/variance out of bounds")
	case p.TypoRate < 0 || p.TypoRate > maxHumanTypoRate:
		return errors.New("human input: typo rate out of bounds")
	case p.ScrollSteps < 0 || p.ScrollSteps > maxHumanScrollSteps:
		return errors.New("human input: scroll steps out of bounds")
	case p.ScrollDuration < 0 || p.ScrollDuration > maxHumanScrollDuration:
		return errors.New("human input: scroll duration out of bounds")
	}
	return nil
}

type humanRandom interface {
	Float64() float64
	Intn(n int) int
	NormFloat64() float64
}

func (p *HumanProfile) random() humanRandom {
	if p.Seed != 0 {
		return rand.New(rand.NewSource(p.Seed)) // #nosec G404 -- reproducible input timing, not security material.
	}
	return random.Source{}
}

// sleepContext waits d unless ctx ends first.
func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// cleanupContext keeps a release/keyUp deliverable after the caller's context
// ended, so cancellation never leaves a pressed button or key behind.
func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), time.Second)
}

func (r *Runtime) pointerPosition() input.MousePoint {
	r.pointerMu.Lock()
	defer r.pointerMu.Unlock()
	return r.pointerAt
}

func (r *Runtime) movePointer(ctx context.Context, point input.MousePoint) error {
	if err := r.pointer.MouseMoveContext(ctx, point.X, point.Y); err != nil {
		return err
	}
	r.pointerMu.Lock()
	r.pointerAt = point
	r.pointerMu.Unlock()
	return nil
}

// pressAndRelease presses at press, holds and always releases at release.
func (r *Runtime) pressAndRelease(ctx context.Context, press, release input.MousePoint, button cdpops.MouseButton, hold time.Duration) error {
	if err := r.pointer.DispatchMouseContext(ctx, cdpops.MouseEvent{EventType: "mousePressed", X: press.X, Y: press.Y, Button: button, ClickCount: 1}); err != nil {
		return err
	}
	holdErr := sleepContext(ctx, hold)
	releaseCtx, cancel := ctx, context.CancelFunc(func() {})
	if holdErr != nil {
		releaseCtx, cancel = cleanupContext(ctx)
	}
	defer cancel()
	releaseErr := r.pointer.DispatchMouseContext(releaseCtx, cdpops.MouseEvent{EventType: "mouseReleased", X: release.X, Y: release.Y, Button: button, ClickCount: 1})
	return errors.Join(holdErr, releaseErr)
}

// humanClick follows the spec click sequence: Bezier approach with frame
// timing, pre-click delay, press at an offset target, hold, jittered release.
func (r *Runtime) humanClick(ctx context.Context, x, y float64, button cdpops.MouseButton, profile *HumanProfile) error {
	rng := profile.random()
	sequence := input.GenerateClickSequence(r.pointerPosition(), input.MousePoint{X: x, Y: y}, input.DefaultClickSequenceConfig(), rng)
	var path []input.MousePoint
	if sequence.MovePath != nil {
		path = sequence.MovePath.Points
	}
	if len(profile.MovePath) > 0 {
		path = profile.MovePath
		sequence.TargetPoint = path[len(path)-1]
		sequence.ReleasePoint = sequence.TargetPoint
	}
	for _, point := range path {
		if err := r.movePointer(ctx, point); err != nil {
			return err
		}
		if err := sleepContext(ctx, input.FrameInterval(rng)); err != nil {
			return err
		}
	}
	if err := r.movePointer(ctx, sequence.TargetPoint); err != nil {
		return err
	}
	preClick := sequence.PreClickDelay
	if profile.PreClickDelay > 0 {
		preClick = profile.PreClickDelay
	}
	if err := sleepContext(ctx, preClick); err != nil {
		return err
	}
	return r.pressAndRelease(ctx, sequence.TargetPoint, sequence.ReleasePoint, button, sequence.HoldDuration)
}

// humanKeys expands text into keystrokes (with corrected typos) and delays.
func humanKeys(text string, profile *HumanProfile, rng humanRandom) ([]rune, []time.Duration) {
	keys := []rune(text)
	delays := input.TypingDelays(text, input.TypingConfig{SigmaPct: 0.15}, rng)
	if profile.TypoRate > 0 {
		delays, keys = input.TypingRhythm(text, input.TypingConfig{TypoRate: profile.TypoRate, SigmaPct: 0.15}, rng)
		for i, key := range keys {
			// The rhythm marks a typo by repeating the intended key after a
			// Backspace; make the key before that Backspace an actual wrong key.
			if key == '\b' && i > 0 && i+1 < len(keys) {
				keys[i-1] = wrongKey(keys[i-1], rng)
			}
		}
	}
	if profile.KeyDelay > 0 {
		for i := range delays {
			offset := time.Duration(0)
			if profile.KeyVariance > 0 {
				offset = time.Duration(rng.Intn(int(2*profile.KeyVariance)+1)) - profile.KeyVariance
			}
			delays[i] = max(profile.KeyDelay+offset, 0)
		}
	}
	return keys, delays
}

func wrongKey(intended rune, rng humanRandom) rune {
	wrong := rune('a' + rng.Intn(26))
	if wrong == intended {
		wrong = 'a' + (wrong-'a'+1)%26
	}
	return wrong
}

// dispatchKey sends one keystroke and always delivers keyUp after keyDown.
// beforeChar runs after keyDown handlers finished; if it fails (for example
// focus moved to another recipient) the character is not sent.
func (r *Runtime) dispatchKey(ctx context.Context, key rune, beforeChar func(context.Context) error) error {
	down := dispatchKeyEventParams{Type: "rawKeyDown", Key: string(key)}
	char := &dispatchKeyEventParams{Type: "char", Text: string(key)}
	up := dispatchKeyEventParams{Type: "keyUp", Key: string(key)}
	if key == '\b' {
		down = dispatchKeyEventParams{Type: "rawKeyDown", Key: "Backspace", Code: "Backspace", WindowsVirtualKeyCode: 8}
		char = nil
		up = dispatchKeyEventParams{Type: "keyUp", Key: "Backspace", Code: "Backspace", WindowsVirtualKeyCode: 8}
	}
	if err := r.page.Call(ctx, "Input.dispatchKeyEvent", down, &emptyResult{}); err != nil {
		return err
	}
	var charErr error
	if char != nil && beforeChar != nil {
		charErr = beforeChar(ctx)
	}
	if char != nil && charErr == nil {
		charErr = r.page.Call(ctx, "Input.dispatchKeyEvent", *char, &emptyResult{})
	}
	upCtx, cancel := ctx, context.CancelFunc(func() {})
	if ctx.Err() != nil {
		upCtx, cancel = cleanupContext(ctx)
	}
	defer cancel()
	return errors.Join(charErr, r.page.Call(upCtx, "Input.dispatchKeyEvent", up, &emptyResult{}))
}

// humanScrollDeltas splits the requested delta into eased wheel steps.
func humanScrollDeltas(deltaX, deltaY float64, profile *HumanProfile) (steps []input.MousePoint, interval time.Duration) {
	count := profile.ScrollSteps
	if count == 0 {
		count = defaultHumanScrollStep
	}
	duration := profile.ScrollDuration
	if duration == 0 {
		duration = defaultHumanScrollTime
	}
	xs := ComputeScrollSteps(int(deltaX), count)
	ys := ComputeScrollSteps(int(deltaY), count)
	steps = make([]input.MousePoint, 0, count)
	for i := range count {
		if xs[i] != 0 || ys[i] != 0 {
			steps = append(steps, input.MousePoint{X: float64(xs[i]), Y: float64(ys[i])})
		}
	}
	return steps, duration / time.Duration(count)
}
