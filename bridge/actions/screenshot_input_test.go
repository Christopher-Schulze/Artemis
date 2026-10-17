package actions

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"sync"
	"testing"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/platform"
)

type screenshotInputOwner struct {
	mu         sync.Mutex
	held       int64
	peak       int64
	limit      int64
	releaseErr error
}

func (o *screenshotInputOwner) ReserveMemory(ctx context.Context, size int64) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit := o.limit
	if limit == 0 {
		limit = 512 << 20
	}
	if size < 1 || size > limit {
		return nil, errors.New("test input capacity denied")
	}
	o.mu.Lock()
	if size > limit-o.held {
		o.mu.Unlock()
		return nil, errors.New("test aggregate capacity denied")
	}
	o.held += size
	o.peak = max(o.peak, o.held)
	o.mu.Unlock()
	var once sync.Once
	return func() error { once.Do(func() { o.mu.Lock(); o.held -= size; o.mu.Unlock() }); return o.releaseErr }, nil
}

func noisyScreenshotJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	value := uint32(97)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			value = value*1664525 + 1013904223
			img.SetRGBA(x, y, color.RGBA{R: byte(value), G: byte(value >> 8), B: byte(value >> 16), A: 255})
		}
	}
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	if raw.Len() <= 1<<20 {
		t.Fatalf("fixture does not reach mmap path: %d bytes", raw.Len())
	}
	return raw.Bytes()
}

func TestModelScreenshotInputAdmissionReportsActualDeniedMetrics(t *testing.T) {
	owner := &screenshotInputOwner{limit: 1}
	raw := noisyScreenshotJPEG(t, 1536, 1024)
	runtime := &Runtime{inputMemory: owner}
	var event bridge.ImageMetricEvent
	ctx, operation := bridge.BeginImageOperation(t.Context(), bridge.ImageOperationCapture, bridge.FormatJPEG, func(observation bridge.ImageMetricEvent) { event = observation })
	outcome := runtime.screenshotInputOutcome(ctx, Request{Kind: KindScreenshot, ForModel: true}, "jpeg", base64.StdEncoding.EncodeToString(raw), Evidence{})
	operation.Finish(ctx, screenshotMetricOutcome(outcome))
	if outcome.Success || outcome.Failure != FailurePolicy || len(outcome.Bytes) != 0 || owner.held != 0 || event.Outcome != bridge.ImageOutcomeDenied || event.CaptureAttempts != 0 || event.DecodeCounts != [3]uint32{} {
		t.Fatalf("input admission lost denial classification or fabricated work: %+v %+v", outcome, event)
	}
}

func TestModelScreenshotMappedInputHasOwnedOutputAfterRelease(t *testing.T) {
	owner := &screenshotInputOwner{}
	raw := noisyScreenshotJPEG(t, 1536, 1024)
	runtime := &Runtime{inputMemory: owner}
	request := Request{Kind: KindScreenshot, ForModel: true}
	outcome := runtime.screenshotInputOutcome(t.Context(), request, "jpeg", base64.StdEncoding.EncodeToString(raw), Evidence{})
	if !outcome.Success || !outcome.ModelPrepared || outcome.InputMemory == nil || outcome.Width != 1024 || outcome.Height > 1024 || owner.held != 0 {
		t.Fatalf("input mapping leaked or model failed: %+v held=%d", outcome, owner.held)
	}
	if platform.Detect().HasMmap && (outcome.InputMemory.State != platform.StateAvailable || owner.peak == 0) {
		t.Fatalf("mapped path not executed: %+v", outcome.InputMemory)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(outcome.Bytes))
	if err != nil || decoded.Bounds().Dx() != outcome.Width || decoded.Bounds().Dy() != outcome.Height {
		t.Fatalf("output lost after unmapping: %v", err)
	}
}

func TestModelScreenshotUnchangedMappedBytesAreTransferredToOwnedOutput(t *testing.T) {
	owner := &screenshotInputOwner{}
	raw := noisyScreenshotJPEG(t, 1024, 1024)
	runtime := &Runtime{inputMemory: owner}
	outcome := runtime.screenshotInputOutcome(t.Context(), Request{Kind: KindScreenshot, ForModel: true}, "jpeg", base64.StdEncoding.EncodeToString(raw), Evidence{})
	if !outcome.Success || !bytes.Equal(outcome.Bytes, raw) || owner.held != 0 {
		t.Fatalf("unchanged output did not survive input retirement: %+v", outcome)
	}
	if _, err := jpeg.Decode(bytes.NewReader(outcome.Bytes)); err != nil {
		t.Fatal(err)
	}
}

func TestScreenshotInputRejectsTypedNilOwnerBeforeConsumer(t *testing.T) {
	var owner *screenshotInputOwner
	called := false
	err := bridge.NewJPEGPipeline().WithScreenshotInput(t.Context(), "YWJj", owner, func([]byte, platform.ProbeResult) error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatalf("typed-nil authority bypassed admission: %v", err)
	}
}

func TestOrdinaryScreenshotInputPreservesActualBytesAndAdmission(t *testing.T) {
	for _, format := range []string{"jpeg", "png"} {
		t.Run(format, func(t *testing.T) {
			raw := screenshotPixels(t, format, 64, 32)
			for _, limit := range []int64{0, 1} {
				owner := &screenshotInputOwner{limit: limit}
				runtime := &Runtime{inputMemory: owner}
				var event bridge.ImageMetricEvent
				ctx, operation := bridge.BeginImageOperation(t.Context(), bridge.ImageOperationCapture, bridge.ScreenshotFormat(format), func(observation bridge.ImageMetricEvent) { event = observation })
				outcome := runtime.screenshotInputOutcome(ctx, Request{Kind: KindScreenshot}, format, base64.StdEncoding.EncodeToString(raw), Evidence{})
				operation.Finish(ctx, screenshotMetricOutcome(outcome))
				if owner.held != 0 {
					t.Fatal("input capacity remained held after return")
				}
				if limit == 1 {
					if outcome.Success || outcome.Failure != FailurePolicy || len(outcome.Bytes) != 0 || owner.peak != 0 || event.DecodeCounts != [3]uint32{} || event.Outcome != bridge.ImageOutcomeDenied {
						t.Fatalf("ordinary input bypassed pre-decode denial: %+v %+v", outcome, event)
					}
					continue
				}
				if !outcome.Success || !bytes.Equal(outcome.Bytes, raw) || outcome.Width != 64 || outcome.Height != 32 || outcome.MIME != "image/"+format || outcome.ModelPrepared || outcome.InputMemory == nil || owner.peak <= int64(len(raw)) {
					t.Fatalf("ordinary output or owned input changed: %+v peak=%d", outcome, owner.peak)
				}
				if _, _, err := image.Decode(bytes.NewReader(outcome.Bytes)); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

type screenshotTransferOwner struct {
	screenshotInputOwner
	calls       int
	transfer    int64
	heldAtCopy  int64
	heldAtClose int64
	deny        bool
	cancel      context.CancelFunc
	transferErr error
}

func (o *screenshotTransferOwner) ReserveMemory(ctx context.Context, size int64) (func() error, error) {
	o.calls++
	if o.calls != 3 {
		return o.screenshotInputOwner.ReserveMemory(ctx, size)
	}
	if o.deny {
		return nil, errors.New("output transfer denied")
	}
	release, err := o.screenshotInputOwner.ReserveMemory(ctx, size)
	if err != nil {
		return release, err
	}
	o.transfer, o.heldAtCopy = size, o.held
	if o.cancel != nil {
		o.cancel()
	}
	return func() error {
		o.heldAtClose = o.held
		return errors.Join(release(), o.transferErr)
	}, nil
}

func TestScreenshotMappedTransferAdmitsOverlapAndJoinsCleanup(t *testing.T) {
	raw := noisyScreenshotJPEG(t, 1024, 1024)
	for _, forModel := range []bool{false, true} {
		for _, tc := range []struct {
			name    string
			deny    bool
			cancel  bool
			settle  bool
			failure FailureClass
		}{
			{name: "retained bytes"},
			{name: "denied copy", deny: true, failure: FailurePolicy},
			{name: "canceled copy", cancel: true, failure: FailureCancelled},
			{name: "failed settlement", settle: true, failure: FailurePostcondition},
		} {
			t.Run(fmt.Sprintf("model=%t/%s", forModel, tc.name), func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				owner := &screenshotTransferOwner{deny: tc.deny}
				if tc.cancel {
					owner.cancel = cancel
				}
				if tc.settle {
					owner.transferErr = errors.New("output transfer settlement failed")
				}
				outcome := (&Runtime{inputMemory: owner}).screenshotInputOutcome(ctx, Request{Kind: KindScreenshot, ForModel: forModel}, "jpeg", base64.StdEncoding.EncodeToString(raw), Evidence{})
				if owner.held != 0 {
					t.Fatalf("transfer leaked capacity: %d", owner.held)
				}
				if !platform.Detect().HasMmap {
					if !outcome.Success || !bytes.Equal(outcome.Bytes, raw) || owner.calls != 2 {
						t.Fatalf("Go fallback changed unchanged bytes: %+v calls=%d", outcome, owner.calls)
					}
					return
				}
				if owner.calls != 3 {
					t.Fatalf("copy did not consult memory owner: %d", owner.calls)
				}
				if tc.failure != "" {
					if outcome.Success || outcome.Failure != tc.failure || len(outcome.Bytes) != 0 || outcome.DecodePath != "" {
						t.Fatalf("failed transfer published pixels: %+v", outcome)
					}
				} else if !outcome.Success || !bytes.Equal(outcome.Bytes, raw) || outcome.Width != 1024 || outcome.Height != 1024 {
					t.Fatalf("owned bytes lost after input unmapping: %+v", outcome)
				}
				if !tc.deny && (owner.transfer != int64(len(raw)) || owner.heldAtCopy <= owner.transfer || owner.heldAtClose != owner.transfer) {
					t.Fatalf("transfer overlap or retirement order incorrect: %+v", owner)
				}
			})
		}
	}
}

func TestScreenshotInputInvalidContextAndBase64PublishNoPixels(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		encoded string
		failure FailureClass
	}{
		{name: "missing context", encoded: "!!!!", failure: FailureValidation},
		{name: "malformed ordinary data", ctx: t.Context(), encoded: "!!!!", failure: FailureProtocol},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := &screenshotInputOwner{}
			outcome := (&Runtime{inputMemory: owner}).screenshotInputOutcome(tc.ctx, Request{Kind: KindScreenshot}, "png", tc.encoded, Evidence{})
			if outcome.Success || outcome.Failure != tc.failure || len(outcome.Bytes) != 0 || owner.held != 0 {
				t.Fatalf("invalid screenshot input published output: %+v held=%d", outcome, owner.held)
			}
		})
	}
}
