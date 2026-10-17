//go:build darwin && cgo && !artemis_libjpeg

package jpegcodec

/*
#cgo LDFLAGS: -framework VideoToolbox -framework CoreMedia -framework CoreVideo -framework CoreFoundation
#include <VideoToolbox/VideoToolbox.h>
#include <CoreMedia/CoreMedia.h>
#include <CoreVideo/CoreVideo.h>
#include <stdint.h>

typedef struct {
    uint8_t *pixels;
    int width, height;
    OSStatus status;
    int completed;
} artemis_jpeg_output;

static void artemis_jpeg_callback(void *owner, void *frame, OSStatus status,
    VTDecodeInfoFlags flags, CVImageBufferRef image, CMTime timestamp, CMTime duration) {
    artemis_jpeg_output *out = owner;
    if (out == NULL) return;
    out->status = status;
    if (status != noErr || image == NULL || (flags & kVTDecodeInfo_FrameDropped)) return;
    if (out->pixels == NULL || CVPixelBufferGetPixelFormatType(image) != kCVPixelFormatType_32BGRA ||
        CVPixelBufferGetWidth(image) != (size_t)out->width ||
        CVPixelBufferGetHeight(image) != (size_t)out->height) {
        out->status = kVTVideoDecoderBadDataErr;
        return;
    }
    CVReturn lock = CVPixelBufferLockBaseAddress(image, kCVPixelBufferLock_ReadOnly);
    if (lock != kCVReturnSuccess) { out->status = lock; return; }
    const uint8_t *src = CVPixelBufferGetBaseAddress(image);
    size_t stride = CVPixelBufferGetBytesPerRow(image);
    if (src == NULL || stride < (size_t)out->width * 4) {
        out->status = kVTVideoDecoderBadDataErr;
    } else {
        for (int y = 0; y < out->height; y++) {
            const uint8_t *row = src + (size_t)y * stride;
            uint8_t *dst = out->pixels + (size_t)y * out->width * 4;
            for (int x = 0; x < out->width; x++) {
                dst[x*4] = row[x*4+2]; dst[x*4+1] = row[x*4+1];
                dst[x*4+2] = row[x*4]; dst[x*4+3] = 255;
            }
        }
        out->completed = 1;
    }
    CVReturn unlock = CVPixelBufferUnlockBaseAddress(image, kCVPixelBufferLock_ReadOnly);
    if (unlock != kCVReturnSuccess) { out->status = unlock; out->completed = 0; }
}

static int artemis_jpeg_supported(void) {
    return VTIsHardwareDecodeSupported(kCMVideoCodecType_JPEG);
}

static OSStatus artemis_jpeg_probe(void) {
    if (!artemis_jpeg_supported()) return kVTVideoDecoderNotAvailableNowErr;
    CMVideoFormatDescriptionRef format = NULL;
    VTDecompressionSessionRef session = NULL;
    CFDictionaryRef spec = NULL;
    CFTypeRef hardware = NULL;
    artemis_jpeg_output output = {NULL, 1280, 640, noErr, 0};
    OSStatus status = CMVideoFormatDescriptionCreate(NULL, kCMVideoCodecType_JPEG, 1280, 640, NULL, &format);
    if (status != noErr) goto cleanup;
    const void *keys[] = {kVTVideoDecoderSpecification_RequireHardwareAcceleratedVideoDecoder};
    const void *values[] = {kCFBooleanTrue};
    spec = CFDictionaryCreate(NULL, keys, values, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    if (spec == NULL) { status = memFullErr; goto cleanup; }
    VTDecompressionOutputCallbackRecord callback = {artemis_jpeg_callback, &output};
    status = VTDecompressionSessionCreate(NULL, format, spec, NULL, &callback, &session);
    if (status != noErr) goto cleanup;
    status = VTSessionCopyProperty(session, kVTDecompressionPropertyKey_UsingHardwareAcceleratedVideoDecoder, NULL, &hardware);
    if (status == noErr && (hardware == NULL || CFGetTypeID(hardware) != CFBooleanGetTypeID() || !CFBooleanGetValue(hardware)))
        status = kVTVideoDecoderNotAvailableNowErr;
cleanup:
    if (session != NULL) { VTDecompressionSessionInvalidate(session); CFRelease(session); }
    if (hardware != NULL) CFRelease(hardware);
    if (spec != NULL) CFRelease(spec);
    if (format != NULL) CFRelease(format);
    return status;
}

static OSStatus artemis_jpeg_decode(uint8_t *raw, size_t size, int width, int height, uint8_t *pixels) {
    CMVideoFormatDescriptionRef format = NULL;
    CMBlockBufferRef block = NULL;
    CMSampleBufferRef sample = NULL;
    VTDecompressionSessionRef session = NULL;
    CFDictionaryRef spec = NULL, attributes = NULL;
    CFNumberRef pixel_format = NULL;
    CFTypeRef hardware = NULL;
    OSStatus status = kVTVideoDecoderNotAvailableNowErr;
    artemis_jpeg_output output = {pixels, width, height, noErr, 0};
    if (!artemis_jpeg_supported()) goto cleanup;
    status = CMVideoFormatDescriptionCreate(NULL, kCMVideoCodecType_JPEG, width, height, NULL, &format);
    if (status != noErr) goto cleanup;
    const void *spec_keys[] = {kVTVideoDecoderSpecification_RequireHardwareAcceleratedVideoDecoder};
    const void *spec_values[] = {kCFBooleanTrue};
    spec = CFDictionaryCreate(NULL, spec_keys, spec_values, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    int32_t pixel_type = kCVPixelFormatType_32BGRA;
    pixel_format = CFNumberCreate(NULL, kCFNumberSInt32Type, &pixel_type);
    if (spec == NULL || pixel_format == NULL) { status = memFullErr; goto cleanup; }
    const void *attribute_keys[] = {kCVPixelBufferPixelFormatTypeKey};
    const void *attribute_values[] = {pixel_format};
    attributes = CFDictionaryCreate(NULL, attribute_keys, attribute_values, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    if (attributes == NULL) { status = memFullErr; goto cleanup; }
    VTDecompressionOutputCallbackRecord callback = {artemis_jpeg_callback, &output};
    status = VTDecompressionSessionCreate(NULL, format, spec, attributes, &callback, &session);
    if (status != noErr) goto cleanup;
    status = VTSessionCopyProperty(session, kVTDecompressionPropertyKey_UsingHardwareAcceleratedVideoDecoder, NULL, &hardware);
    if (status != noErr) goto cleanup;
    if (hardware == NULL || CFGetTypeID(hardware) != CFBooleanGetTypeID() || !CFBooleanGetValue(hardware)) {
        status = kVTVideoDecoderNotAvailableNowErr; goto cleanup;
    }
    // The block borrows Go input; kCFAllocatorNull prevents any native free.
    status = CMBlockBufferCreateWithMemoryBlock(NULL, raw, size, kCFAllocatorNull, NULL, 0, size, 0, &block);
    if (status != noErr) goto cleanup;
    status = CMSampleBufferCreateReady(NULL, block, format, 1, 0, NULL, 1, &size, &sample);
    if (status != noErr) goto cleanup;
    // Both asynchronous and temporal flags are clear: Apple guarantees that
    // decompression and this callback complete before DecodeFrame returns.
    status = VTDecompressionSessionDecodeFrame(session, sample, 0, NULL, NULL);
    if (status == noErr) status = output.status;
    if (status == noErr && !output.completed) status = kVTVideoDecoderBadDataErr;
cleanup:
    if (session != NULL) { VTDecompressionSessionInvalidate(session); CFRelease(session); }
    if (sample != NULL) CFRelease(sample);
    if (block != NULL) CFRelease(block);
    if (hardware != NULL) CFRelease(hardware);
    if (attributes != NULL) CFRelease(attributes);
    if (pixel_format != NULL) CFRelease(pixel_format);
    if (spec != NULL) CFRelease(spec);
    if (format != NULL) CFRelease(format);
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

const nativePath = "hw_videotoolbox"

func probeNative() Availability {
	status := C.artemis_jpeg_probe()
	available := status == 0
	reason := fmt.Sprintf("VideoToolbox hardware JPEG session admission rejected: OSStatus %d", int64(status))
	if available {
		reason = "linked VideoToolbox admitted a JPEG hardware session and hardware property; each decode repeats admission and must produce pixels"
	}
	failed := status != 0 && status != C.kVTPropertyNotSupportedErr && status != C.kVTVideoDecoderUnsupportedDataFormatErr
	return Availability{Available: available, ObservationFailed: failed, Path: nativePath, Reason: reason}
}

func decodeNative(raw []byte, width, height int) (image.Image, error) {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	status := C.artemis_jpeg_decode((*C.uint8_t)(unsafe.Pointer(&raw[0])), C.size_t(len(raw)), C.int(width), C.int(height), (*C.uint8_t)(unsafe.Pointer(&img.Pix[0])))
	runtime.KeepAlive(raw)
	runtime.KeepAlive(img)
	if status != 0 {
		return nil, fmt.Errorf("VideoToolbox JPEG decode: OSStatus %d", int64(status))
	}
	return img, nil
}
