package bridge

import (
	"encoding/base64"
	"fmt"
	"runtime"
	"strings"
	"unsafe"

	fastbase64 "github.com/cristalhq/base64"
)

// ScreenshotDataError identifies invalid CDP base64 data, not capacity denial.
type ScreenshotDataError struct {
	Cause error
}

func (e *ScreenshotDataError) Error() string { return e.Cause.Error() }
func (e *ScreenshotDataError) Unwrap() error { return e.Cause }

// DecodeScreenshotData decodes bounded CDP image data into one owned output.
// The fast decoder is scalar/table-based. It neither proves SIMD nor makes
// image decoding allocation-free. CR/LF inputs retain stdlib compatibility.
func DecodeScreenshotData(encoded string) ([]byte, error) {
	if len(encoded) == 0 || len(encoded) > JPEGEncodedLimit {
		return nil, fmt.Errorf("screenshot encoded data exceeds byte budget")
	}
	raw := make([]byte, fastbase64.StdEncoding.DecodedLen(len(encoded)))
	n, err := DecodeScreenshotDataInto(raw, encoded)
	if err != nil {
		return nil, err
	}
	return raw[:n], nil
}

// DecodeScreenshotDataInto borrows immutable encoded input during the call and
// writes into caller-owned, non-overlapping output. A valid call allocates no
// input/output buffer; callers retain ownership until decoding has returned.
// dst must fit cristalhq/base64's DecodedLen, including its guard byte.
func DecodeScreenshotDataInto(dst []byte, encoded string) (int, error) {
	if len(encoded) == 0 || len(encoded) > JPEGEncodedLimit {
		return 0, fmt.Errorf("screenshot encoded data exceeds byte budget")
	}
	if len(dst) < fastbase64.StdEncoding.DecodedLen(len(encoded)) {
		return 0, fmt.Errorf("screenshot output buffer too small")
	}
	// Both pinned decoder implementations only read src. Go's StringData/Slice
	// APIs keep its bytes borrowed without a whole CDP-string copy; src is never
	// exposed or retained, and neither decoder receives permission to mutate it.
	src := unsafe.Slice(unsafe.StringData(encoded), len(encoded))
	var n int
	var err error
	if strings.IndexByte(encoded, '\r') >= 0 || strings.IndexByte(encoded, '\n') >= 0 {
		n, err = base64.StdEncoding.Decode(dst, src)
	} else {
		n, err = fastbase64.StdEncoding.Decode(dst, src)
	}
	runtime.KeepAlive(encoded)
	if err != nil {
		return 0, &ScreenshotDataError{Cause: fmt.Errorf("decode screenshot base64: %w", err)}
	}
	if n == 0 || n > JPEGEncodedLimit/4*3 {
		return 0, &ScreenshotDataError{Cause: fmt.Errorf("screenshot decoded data exceeds byte budget")}
	}
	return n, nil
}
