//go:build (linux || darwin) && cgo && artemis_libjpeg

package jpegcodec

/*
#cgo pkg-config: libturbojpeg
#include <turbojpeg.h>
#include <stdint.h>
#include <stdio.h>

static int artemis_jpeg_supported(void) {
    tjhandle decoder = tj3Init(TJINIT_DECOMPRESS);
    if (decoder == NULL) return 0;
    tj3Destroy(decoder);
    return 1;
}

static int artemis_jpeg_decode(const uint8_t *raw, size_t size, int width, int height,
    uint8_t *pixels, char *error, size_t error_size) {
    tjhandle decoder = tj3Init(TJINIT_DECOMPRESS);
    if (decoder == NULL) {
        snprintf(error, error_size, "TurboJPEG instance admission failed");
        return -1;
    }
    int status = -1;
    if (tj3Set(decoder, TJPARAM_STOPONWARNING, 1) < 0 ||
        tj3Set(decoder, TJPARAM_SCANLIMIT, 100) < 0 ||
        tj3Set(decoder, TJPARAM_MAXMEMORY, 128) < 0 ||
        tj3Set(decoder, TJPARAM_MAXPIXELS, 32*1024*1024) < 0 ||
        tj3DecompressHeader(decoder, raw, size) < 0) goto cleanup;
    if (tj3Get(decoder, TJPARAM_JPEGWIDTH) != width ||
        tj3Get(decoder, TJPARAM_JPEGHEIGHT) != height) {
        snprintf(error, error_size, "JPEG dimensions disagree with validated Go header");
        tj3Destroy(decoder);
        return -1;
    }
    status = tj3Decompress8(decoder, raw, size, pixels, width*4, TJPF_RGBA);
cleanup:
    if (status < 0) snprintf(error, error_size, "%s", tj3GetErrorStr(decoder));
    tj3Destroy(decoder);
    return status;
}
*/
import "C"

import (
	"fmt"
	"image"
	"runtime"
	"unsafe"
)

const nativePath = "hw_libjpeg_turbo"

func probeNative() Availability {
	available := C.artemis_jpeg_supported() != 0
	reason := "linked libjpeg-turbo could not create a decompressor"
	if available {
		reason = "linked libjpeg-turbo admitted a decompressor; this is native CPU decoding, not GPU hardware proof"
	}
	return Availability{Available: available, ObservationFailed: !available, Path: nativePath, Reason: reason}
}

func decodeNative(raw []byte, width, height int) (image.Image, error) {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	var message [512]C.char
	status := C.artemis_jpeg_decode((*C.uint8_t)(unsafe.Pointer(&raw[0])), C.size_t(len(raw)), C.int(width), C.int(height), (*C.uint8_t)(unsafe.Pointer(&img.Pix[0])), &message[0], C.size_t(len(message)))
	runtime.KeepAlive(raw)
	runtime.KeepAlive(img)
	if status != 0 {
		return nil, fmt.Errorf("libjpeg-turbo JPEG decode: %s", C.GoString(&message[0]))
	}
	return img, nil
}
