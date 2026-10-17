package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// decodeScopedResponse retains every admitted layer through borrowed consumption.
// Codec construction and joined close precede workspace settlement.
func (c *HTTPClient) decodeScopedResponse(ctx context.Context, body []byte, encoding string, limit int64) ([]byte, func() error, error) {
	if c.cfg.MemoryAdmission == nil {
		decoded, err := decodeResponseBody(body, encoding, limit)
		return decoded, nil, err
	}
	if limit <= 0 || int64(len(body)) > limit {
		return nil, nil, ErrBodyTooLarge
	}
	layers := strings.Split(encoding, ",")
	if len(layers) > 4 {
		return nil, nil, errors.New("too many content-encoding layers")
	}
	releases := make([]func() error, 0, len(layers))
	for index := len(layers) - 1; index >= 0; index-- {
		decoded, release, err := c.decodeAdmittedLayer(ctx, body, strings.ToLower(strings.TrimSpace(layers[index])), limit)
		releases = append(releases, release)
		if err != nil {
			return nil, releaseResponseInputs(releases...), err
		}
		body = decoded
	}
	return body, releaseResponseInputs(releases...), ctx.Err()
}

func (c *HTTPClient) decodeAdmittedLayer(ctx context.Context, body []byte, encoding string, limit int64) (decoded []byte, outputRelease func() error, err error) {
	workspace, err := responseDecoderWorkspace(encoding, limit)
	if err != nil {
		return nil, nil, err
	}
	if workspace == 0 {
		return body, nil, ctx.Err()
	}
	release, admissionErr := c.cfg.MemoryAdmission.ReserveMemory(ctx, workspace)
	if release != nil {
		defer func() { err = errors.Join(err, release()) }()
	}
	if admissionErr != nil {
		return nil, nil, fmt.Errorf("admit response decoder workspace: %w", admissionErr)
	}
	if release == nil {
		return nil, nil, fmt.Errorf("response decoder requires a release owner")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	decoded, err = decodeResponseLayerWithReader(body, encoding, limit, func(reader io.Reader, limit int64) ([]byte, error) {
		var readErr error
		decoded, outputRelease, readErr = readAdmittedResponse(ctx, reader, -1, limit, c.cfg.MemoryAdmission)
		return decoded, readErr
	})
	return decoded, outputRelease, errors.Join(err, ctx.Err())
}

func responseDecoderWorkspace(encoding string, limit int64) (int64, error) {
	switch encoding {
	case "", "identity":
		return 0, nil
	case "gzip", "x-gzip", "deflate":
		return 1 << 20, nil // inflate window, buffered input and Huffman state
	case "br":
		return 64 << 20, nil // 24-bit window, replacement overlap and tables
	case "zstd":
		// The pinned single-worker decoder admits at most limit+1 window bytes.
		// Count both 2x-window history generations plus block/table state.
		if limit <= 0 || limit > (int64(^uint(0)>>1)-(2<<20))/4-1 {
			return 0, fmt.Errorf("response decoder workspace exceeds bounded capacity")
		}
		return 4*(limit+1) + (2 << 20), nil
	default:
		return 0, fmt.Errorf("unsupported content encoding %q", encoding)
	}
}
