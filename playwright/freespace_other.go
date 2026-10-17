//go:build !unix

package playwright

import "errors"

// availableBytes fails closed on platforms without a supported free-space
// probe, so the lane reports itself unavailable instead of overfilling disk.
func availableBytes(string) (int64, error) {
	return 0, errors.New("free-space probe unsupported on this platform")
}
