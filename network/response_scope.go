package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/Christopher-Schulze/Artemis/platform"
)

// WithResponse consumes borrowed response bytes before joined cleanup. Do not
// retain response.Body after the callback; copy any payload that must outlive it.
// Ordinary Do/DoTarget retain their existing caller-owned Go byte contract.
func (c *HTTPClient) WithResponse(ctx context.Context, r Request, kind TargetKind, consume func(*Response) error) (err error) {
	if consume == nil {
		return fmt.Errorf("network: response consumer required")
	}
	response, err := c.doTarget(ctx, r, kind, true)
	if err != nil {
		return err
	}
	defer func() {
		if response.release != nil {
			err = errors.Join(err, response.release())
		}
		response.Body = nil
	}()
	if err := response.operationCtx.Err(); err != nil {
		return err
	}
	err = consume(response)
	return errors.Join(err, response.operationCtx.Err())
}

func (c *HTTPClient) readScopedBody(ctx context.Context, reader io.Reader, size, limit int64, scoped bool) (body []byte, release func() error, observation platform.ProbeResult, err error) {
	observation = c.mappedBodyFallback(size, scoped)
	if !scoped || size <= 1<<20 || size > platform.MaxMappedBufferBytes || !c.caps.HasMmap || c.cfg.MemoryAdmission == nil {
		if scoped && c.cfg.MemoryAdmission != nil {
			body, release, err = readAdmittedResponse(ctx, reader, size, limit, c.cfg.MemoryAdmission)
			return body, release, observation, err
		}
		body, err = readLimited(reader, limit)
		return body, nil, observation, err
	}
	return c.readMappedInput(ctx, reader, size, limit)
}

func (c *HTTPClient) mappedBodyFallback(size int64, scoped bool) platform.ProbeResult {
	observation := platform.ProbeResult{State: platform.StateUnavailable, Source: "response input owner"}
	switch {
	case !scoped:
		observation.Reason = "ordinary caller-owned value API"
	case size < 0:
		observation.Reason = "response length unknown; bounded ordinary read"
	case size <= 1<<20:
		observation.Reason = "response below mmap threshold"
	case size > platform.MaxMappedBufferBytes:
		observation.Reason = "response above mapped allocation limit; bounded ordinary read"
	case !c.caps.HasMmap:
		observation.Reason = c.caps.Evidence.Mmap.Reason
		if observation.Reason == "" {
			observation.Reason = "mapping support not admitted"
		}
		if c.caps.Evidence.Mmap.State == platform.StateUnknown {
			observation.State = platform.StateUnknown
		}
	case c.cfg.MemoryAdmission == nil:
		observation.Reason = "no mapped memory capacity owner bound"
	}
	return observation
}

func (c *HTTPClient) readMappedInput(ctx context.Context, reader io.Reader, size, limit int64) (body []byte, release func() error, observation platform.ProbeResult, err error) {
	observation = platform.ProbeResult{State: platform.StateUnavailable, Source: "response input owner"}
	if size > limit {
		return nil, nil, observation, ErrBodyTooLarge
	}
	allocationBytes, err := platform.MappedAllocationBytes(int(size))
	if err != nil {
		return nil, nil, observation, err
	}
	releaseMemory, err := c.cfg.MemoryAdmission.ReserveMemory(ctx, allocationBytes)
	if err != nil {
		return nil, nil, observation, fmt.Errorf("admit response memory: %w", err)
	}
	if releaseMemory == nil {
		return nil, nil, observation, fmt.Errorf("response memory admission supplied no release owner")
	}
	mapping, mapErr := platform.NewMappedBuffer(ctx, int(size))
	if mapErr != nil && mapping != nil {
		return nil, releaseMappedInput(mapping, releaseMemory), observation, mapErr
	}
	if mapErr != nil {
		observation.State, observation.Reason = platform.StateDegraded, mapErr.Error()
		if ctx.Err() != nil {
			return nil, nil, observation, errors.Join(ctx.Err(), releaseMemory())
		}
		if err := releaseMemory(); err != nil {
			return nil, nil, observation, errors.Join(mapErr, err)
		}
		body, release, err = readAdmittedResponse(ctx, reader, size, limit, c.cfg.MemoryAdmission)
		return body, release, observation, err
	}
	release = releaseMappedInput(mapping, releaseMemory)
	body = mapping.Bytes()
	if err := readDeclaredBody(ctx, reader, body); err != nil {
		return nil, release, observation, err
	}
	observation = platform.ProbeResult{State: platform.StateAvailable, Source: "owned anonymous response mmap", Reason: "response read directly into admitted mapping; no disk file", Value: uint64(size)}
	return body, release, observation, nil
}

func releaseMappedInput(mapping *platform.MappedBuffer, releaseMemory func() error) func() error {
	var once sync.Once
	var result error
	return func() error {
		once.Do(func() {
			result = mapping.Close()
			if result == nil {
				result = releaseMemory()
			}
		})
		return result
	}
}

func readDeclaredBody(ctx context.Context, reader io.Reader, body []byte) error {
	if _, err := io.ReadFull(reader, body); err != nil {
		return err
	}
	var extra [1]byte
	n, err := io.ReadFull(reader, extra[:])
	if n != 0 {
		return fmt.Errorf("response exceeded declared body size")
	}
	if !errors.Is(err, io.EOF) {
		return fmt.Errorf("response body end: %w", err)
	}
	return ctx.Err()
}
