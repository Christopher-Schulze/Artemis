// Package jpegcodec owns optional native JPEG decoders and their input lifetimes.
package jpegcodec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
)

var ErrUnavailable = errors.New("native JPEG decoder unavailable")

// Availability describes the linked backend and its codec-specific admission.
// Available does not claim that any particular image has been decoded.
type Availability struct {
	Available         bool
	ObservationFailed bool
	Path              string
	Reason            string
}

// Probe checks the exact linked codec, without creating a browser or image file.
func Probe() Availability { return probeNative() }

// Decode borrows raw only until return. Native callbacks and sessions must close
// before returning; only a successfully decoded native image receives its label.
func Decode(ctx context.Context, raw []byte) (image.Image, string, error) {
	if ctx == nil {
		return nil, "", fmt.Errorf("native JPEG: context required")
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if len(raw) == 0 || len(raw) > 24<<20 {
		return nil, "", fmt.Errorf("native JPEG: input exceeds byte budget")
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("native JPEG header: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > (32<<20)/config.Height {
		return nil, "", fmt.Errorf("native JPEG: image exceeds pixel budget")
	}
	if err := ValidateScanBudget(raw); err != nil {
		return nil, "", err
	}
	if !Probe().Available {
		return nil, "", ErrUnavailable
	}
	img, err := decodeNative(raw, config.Width, config.Height)
	if err != nil {
		return nil, "", err
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	return img, nativePath, nil
}
