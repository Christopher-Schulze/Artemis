package network

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

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
	input := bytes.NewReader(body)
	switch encoding {
	case "identity", "":
		return body, nil
	case "br":
		return readLimited(brotli.NewReader(input), limit)
	case "zstd":
		decoder, err := zstd.NewReader(input, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(uint64(limit)+1))
		if err != nil {
			return nil, err
		}
		defer decoder.Close()
		return readLimited(decoder, limit)
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
		decoded, readErr := readLimited(decoder, limit)
		return decoded, errors.Join(readErr, decoder.Close())
	default:
		return nil, fmt.Errorf("unsupported content encoding %q", encoding)
	}
}
