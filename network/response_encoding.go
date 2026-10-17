package network

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// ValidateResponse bounds local fulfillment, including compressed expansion,
// through the same response decoder used by actual HTTP requests.
func (p *Policy) ValidateResponse(ctx context.Context, body []byte, encoding string, kind TargetKind, sessionID string) error {
	if err := p.validateContext(ctx); err != nil {
		return err
	}
	if !validTargetKind(kind) {
		return p.deny(kind, nil, sessionID, "invalid_target_kind")
	}
	limit := p.config.MaxResponseBodyBytes
	if kind == TargetDownload {
		limit = p.config.MaxDownloadBytes
	}
	if int64(len(body)) > limit {
		return errors.Join(ErrBodyTooLarge, p.deny(kind, nil, sessionID, "response_body_too_large"))
	}
	if encoding != "" {
		if _, err := decodeResponseBody(body, encoding, limit); err != nil {
			return errors.Join(err, p.deny(kind, nil, sessionID, "invalid_response_encoding"))
		}
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrPolicyDenied, err)
	}
	return nil
}

// decodeResponseBody bounds every decoded layer independently of wire size.
func decodeResponseBody(body []byte, encoding string, limit int64) ([]byte, error) {
	if limit <= 0 || int64(len(body)) > limit {
		return nil, ErrBodyTooLarge
	}
	layers := strings.Split(encoding, ",")
	if len(layers) > 4 {
		return nil, errors.New("too many content-encoding layers")
	}
	for index := len(layers) - 1; index >= 0; index-- {
		var err error
		body, err = decodeResponseLayer(body, strings.ToLower(strings.TrimSpace(layers[index])), limit)
		if err != nil {
			return body, err
		}
	}
	return body, nil
}

func decodeResponseLayer(body []byte, encoding string, limit int64) ([]byte, error) {
	return decodeResponseLayerWithReader(body, encoding, limit, readLimited)
}

func decodeResponseLayerWithReader(body []byte, encoding string, limit int64, read func(io.Reader, int64) ([]byte, error)) ([]byte, error) {
	input := bytes.NewReader(body)
	switch encoding {
	case "identity", "":
		return body, nil
	case "br":
		return read(brotli.NewReader(input), limit)
	case "zstd":
		decoder, err := zstd.NewReader(input, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(uint64(limit)+1))
		if err != nil {
			return nil, err
		}
		defer decoder.Close()
		return read(decoder, limit)
	case "gzip", "x-gzip", "deflate":
		var decoder io.ReadCloser
		var err error
		if encoding == "deflate" {
			decoder, err = zlib.NewReader(input)
			if errors.Is(err, zlib.ErrHeader) {
				decoder = flate.NewReader(bytes.NewReader(body))
				err = nil
			}
		} else {
			decoder, err = gzip.NewReader(input)
		}
		if err != nil {
			return nil, err
		}
		decoded, readErr := read(decoder, limit)
		return decoded, errors.Join(readErr, decoder.Close())
	default:
		return nil, fmt.Errorf("unsupported content encoding %q", encoding)
	}
}
