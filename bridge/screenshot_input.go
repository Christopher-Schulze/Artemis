package bridge

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/Christopher-Schulze/Artemis/platform"
)

// ScreenshotMemoryAdmission is the embedding owner's transient input port.
type ScreenshotMemoryAdmission interface {
	ReserveMemory(context.Context, int64) (func() error, error)
}

// WithScreenshotInput consumes borrowed decoded image bytes before unmapping.
// Consumers retain derived images or copy any raw bytes that must outlive it.
func (p *JPEGPipeline) WithScreenshotInput(ctx context.Context, encoded string, owner ScreenshotMemoryAdmission, consume func([]byte, platform.ProbeResult) error) error {
	if p == nil || ctx == nil || consume == nil {
		return fmt.Errorf("screenshot input: pipeline, context and consumer required")
	}
	if err := validateScreenshotMemoryOwner(owner); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(encoded) == 0 || len(encoded) > JPEGEncodedLimit {
		return fmt.Errorf("screenshot input exceeds encoded bounds")
	}
	size := len(encoded)/4*3 + 1
	if owner == nil || size <= 1<<20 || p.mmapAdmission.State != platform.StateAvailable {
		return p.consumeGoScreenshot(ctx, encoded, size, owner, consume)
	}
	return p.consumeMappedScreenshot(ctx, encoded, size, owner, consume)
}

func (p *JPEGPipeline) consumeGoScreenshot(ctx context.Context, encoded string, size int, owner ScreenshotMemoryAdmission, consume func([]byte, platform.ProbeResult) error) (err error) {
	if owner != nil {
		release, admissionErr := owner.ReserveMemory(ctx, int64(size))
		if admissionErr != nil {
			return &ImageAdmissionError{Cause: fmt.Errorf("admit Go screenshot input: %w", admissionErr)}
		}
		if release == nil {
			return fmt.Errorf("Go screenshot input admission supplied no release owner")
		}
		defer func() { err = errors.Join(err, release()) }()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := DecodeScreenshotData(encoded)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	observation := platform.ProbeResult{State: platform.StateUnavailable, Source: "owned Go screenshot input", Reason: "mapping not requested or not admitted; bounded Go output reserved when an owner is supplied"}
	if size > 1<<20 && p.mmapAdmission.State == platform.StateUnknown {
		observation.State, observation.Reason = platform.StateUnknown, p.mmapAdmission.Reason
	}
	return errors.Join(consume(data, observation), ctx.Err())
}

func validateScreenshotMemoryOwner(owner ScreenshotMemoryAdmission) error {
	if owner == nil {
		return nil
	}
	value := reflect.ValueOf(owner)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return fmt.Errorf("screenshot input: typed-nil memory admission")
		}
	}
	return nil
}

func (p *JPEGPipeline) consumeMappedScreenshot(ctx context.Context, encoded string, size int, owner ScreenshotMemoryAdmission, consume func([]byte, platform.ProbeResult) error) (err error) {
	capacity, err := platform.MappedAllocationBytes(size)
	if err != nil {
		return err
	}
	release, err := owner.ReserveMemory(ctx, capacity)
	if err != nil {
		return &ImageAdmissionError{Cause: fmt.Errorf("admit screenshot input: %w", err)}
	}
	if release == nil {
		return fmt.Errorf("screenshot input admission supplied no release owner")
	}
	mapping, mapErr := platform.NewMappedBuffer(ctx, size)
	defer func() {
		if mapping != nil {
			if closeErr := mapping.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
				return
			}
		}
		err = errors.Join(err, release())
	}()
	if mapErr != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := DecodeScreenshotData(encoded)
		if err != nil {
			return err
		}
		return errors.Join(consume(data, platform.ProbeResult{State: platform.StateDegraded, Source: "owned Go screenshot input", Reason: mapErr.Error()}), ctx.Err())
	}
	n, err := DecodeScreenshotDataInto(mapping.Bytes(), encoded)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	observation := platform.ProbeResult{State: platform.StateAvailable, Source: "owned anonymous screenshot mmap", Reason: "base64 decoded directly into admitted mapping; no input copy or disk file", Value: uint64(n)}
	return errors.Join(consume(mapping.Bytes()[:n], observation), ctx.Err())
}
