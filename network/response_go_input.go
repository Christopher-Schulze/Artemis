package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

// readAdmittedResponse owns actual Go capacity, including replacement overlap.
// Known lengths allocate once; unknown lengths grow only after admission.
func readAdmittedResponse(ctx context.Context, reader io.Reader, size, limit int64, owner MemoryAdmission) (body []byte, release func() error, err error) {
	if ctx == nil || reader == nil || owner == nil || limit <= 0 || uint64(limit) >= uint64(^uint(0)>>1) {
		return nil, nil, fmt.Errorf("response input: context, reader, owner and bounded limit required")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if size > limit {
		return nil, nil, ErrBodyTooLarge
	}
	if size == 0 {
		return nil, nil, readDeclaredBody(ctx, reader, nil)
	}
	capacity := size
	if capacity < 0 {
		capacity = min(32<<10, limit+1)
	}
	body, release, err = allocateResponseInput(ctx, int(capacity), owner)
	if err != nil {
		return nil, release, err
	}
	if size >= 0 {
		return body, release, readDeclaredBody(ctx, reader, body)
	}
	return readGrowingResponse(ctx, reader, body, release, limit, owner)
}

func readGrowingResponse(ctx context.Context, reader io.Reader, body []byte, release func() error, limit int64, owner MemoryAdmission) ([]byte, func() error, error) {
	used := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, release, err
		}
		n, readErr := io.ReadFull(reader, body[used:])
		used += n
		if int64(used) > limit {
			return nil, release, ErrBodyTooLarge
		}
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
			return body[:used], release, ctx.Err()
		}
		if readErr != nil {
			return nil, release, readErr
		}
		var extra [1]byte
		if _, err := io.ReadFull(reader, extra[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return body[:used], release, ctx.Err()
			}
			return nil, release, err
		}
		if int64(used) >= limit {
			return nil, release, ErrBodyTooLarge
		}
		var growErr error
		body, release, growErr = growResponseInput(ctx, body, release, extra[0], limit, owner)
		used++
		if growErr != nil {
			return nil, release, growErr
		}
	}
}

func growResponseInput(ctx context.Context, body []byte, release func() error, extra byte, limit int64, owner MemoryAdmission) ([]byte, func() error, error) {
	capacity := min(int64(len(body))*2, limit+1)
	next, settleNext, err := allocateResponseInput(ctx, int(capacity), owner)
	if err != nil {
		return nil, release, errors.Join(err, releaseResponseInputs(settleNext)())
	}
	copy(next, body)
	next[len(body)] = extra
	return next, settleNext, release()
}

func allocateResponseInput(ctx context.Context, size int, owner MemoryAdmission) ([]byte, func() error, error) {
	release, err := owner.ReserveMemory(ctx, int64(size))
	if err != nil {
		return nil, release, fmt.Errorf("admit Go response input: %w", err)
	}
	if release == nil {
		return nil, nil, fmt.Errorf("Go response input requires a release owner")
	}
	release = releaseResponseInputs(release)
	if err := ctx.Err(); err != nil {
		return nil, release, err
	}
	return make([]byte, size), release, nil
}

func releaseResponseInputs(releases ...func() error) func() error {
	var once sync.Once
	var result error
	return func() error {
		once.Do(func() {
			for _, release := range releases {
				if release != nil {
					result = errors.Join(result, release())
				}
			}
		})
		return result
	}
}
