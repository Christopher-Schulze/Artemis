//go:build (!darwin && !linux) || !cgo || (linux && !artemis_libjpeg)

package jpegcodec

import "image"

const nativePath = ""

func probeNative() Availability {
	return Availability{Reason: "no native JPEG backend linked; Linux requires cgo and artemis_libjpeg"}
}

func decodeNative([]byte, int, int) (image.Image, error) {
	return nil, ErrUnavailable
}
