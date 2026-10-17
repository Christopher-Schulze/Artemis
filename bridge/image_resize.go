package bridge

// Lanczos weighting, alpha accumulation and rounding are adapted from
// github.com/disintegration/imaging v1.6.2 resize.go and scanner.go.
//
// The MIT License (MIT)
// Copyright (c) 2012 Grigory Dryapak
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"reflect"

	"github.com/disintegration/imaging"
)

type imageResizeWeight struct {
	index  int
	weight float64
}

// ResizeForModelContext preserves fitting input, uses the pinned Lanczos
// kernel for downscaling, and retains CPU ownership through all worker joins.
func (p *JPEGPipeline) ResizeForModelContext(ctx context.Context, img image.Image) (image.Image, error) {
	var result image.Image
	_, err := p.withImageCPU(ctx, func(workCtx context.Context, owned *JPEGPipeline) error {
		var resizeErr error
		result, resizeErr = owned.resizeForModel(workCtx, img, 512<<20)
		return resizeErr
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (p *JPEGPipeline) resizeForModel(ctx context.Context, img image.Image, memoryLimit int64) (image.Image, error) {
	if img == nil || p.MaxWidth <= 0 || p.MaxHeight <= 0 {
		return nil, fmt.Errorf("model resize: image and positive bounds required")
	}
	value := reflect.ValueOf(img)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return nil, fmt.Errorf("model resize: typed-nil image")
		}
	}
	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 || bounds.Dx() > JPEGPixelsLimit/bounds.Dy() {
		return nil, fmt.Errorf("model resize: pixel budget exceeded")
	}
	width, height := modelFitSize(bounds.Dx(), bounds.Dy(), p.MaxWidth, p.MaxHeight)
	if width == bounds.Dx() && height == bounds.Dy() {
		return img, ctx.Err()
	}
	if memoryLimit > 0 && p.modelResizeMemoryBytes(bounds.Dx(), bounds.Dy()) > memoryLimit {
		return nil, fmt.Errorf("model resize: unowned working memory exceeds 512 MiB ceiling")
	}
	var err error
	if width != bounds.Dx() {
		img, err = p.resizeModelAxis(ctx, img, width, true)
		if err != nil {
			return nil, err
		}
	}
	if height != bounds.Dy() {
		img, err = p.resizeModelAxis(ctx, img, height, false)
	}
	return img, err
}

func modelFitSize(width, height, maxWidth, maxHeight int) (int, int) {
	if width <= maxWidth && height <= maxHeight {
		return width, height
	}
	aspect := float64(width) / float64(height)
	if aspect > float64(maxWidth)/float64(maxHeight) {
		return maxWidth, max(1, int(float64(maxWidth)/aspect))
	}
	return max(1, int(float64(maxHeight)*aspect)), maxHeight
}

func modelResizeWeights(ctx context.Context, destination, source int) ([][]imageResizeWeight, error) {
	ratio := float64(source) / float64(destination)
	scale := max(1.0, ratio)
	radius := math.Ceil(scale * imaging.Lanczos.Support)
	weights := make([][]imageResizeWeight, destination)
	storage := make([]imageResizeWeight, 0, destination*int(radius+2)*2)
	for target := range destination {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		center := (float64(target)+0.5)*ratio - 0.5
		begin, end := max(0, int(math.Ceil(center-radius))), min(source-1, int(math.Floor(center+radius)))
		start, sum := len(storage), 0.0
		for index := begin; index <= end; index++ {
			if index&255 == 0 && ctx.Err() != nil {
				return nil, ctx.Err()
			}
			weight := imaging.Lanczos.Kernel((float64(index) - center) / scale)
			if weight != 0 {
				sum += weight
				storage = append(storage, imageResizeWeight{index, weight})
			}
		}
		weights[target] = storage[start:len(storage):len(storage)]
		if sum != 0 {
			for index := range weights[target] {
				weights[target][index].weight /= sum
			}
		}
	}
	return weights, nil
}

func (p *JPEGPipeline) resizeModelAxis(ctx context.Context, img image.Image, destination int, horizontal bool) (image.Image, error) {
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	source, lines := height, width
	output := image.Rect(0, 0, width, destination)
	if horizontal {
		source, lines = width, height
		output = image.Rect(0, 0, destination, height)
	}
	weights, err := modelResizeWeights(ctx, destination, source)
	if err != nil {
		return nil, err
	}
	resized := image.NewNRGBA(output)
	var palette []color.NRGBA
	if source, ok := img.(*image.Paletted); ok {
		palette = make([]color.NRGBA, min(256, len(source.Palette)))
		for index := range palette {
			palette[index] = color.NRGBAModel.Convert(source.Palette[index]).(color.NRGBA)
		}
	}
	err = runImageWorkers(ctx, p.imageWorkerCount(), lines, p.CPU.Affinity, func(workCtx context.Context, first, step int) error {
		scanline := make([]byte, source*4)
		for line := first; line < lines; line += step {
			if err := scanModelImageLine(workCtx, img, line, horizontal, scanline, palette); err != nil {
				return err
			}
			for target, contributions := range weights {
				if target&63 == 0 && workCtx.Err() != nil {
					return workCtx.Err()
				}
				pixel, err := filterModelImagePixel(workCtx, scanline, contributions)
				if err != nil {
					return err
				}
				x, y := line, target
				if horizontal {
					x, y = target, line
				}
				offset := y*resized.Stride + x*4
				destination := resized.Pix[offset : offset+4 : offset+4]
				destination[0], destination[1], destination[2], destination[3] = pixel.R, pixel.G, pixel.B, pixel.A
			}
		}
		return workCtx.Err()
	})
	if err != nil {
		return nil, err
	}
	return resized, nil
}

func filterModelImagePixel(ctx context.Context, scanline []byte, weights []imageResizeWeight) (color.NRGBA, error) {
	var red, green, blue, alpha float64
	for index, contribution := range weights {
		if index != 0 && index&255 == 0 && ctx.Err() != nil {
			return color.NRGBA{}, ctx.Err()
		}
		offset := contribution.index * 4
		pixel := scanline[offset : offset+4 : offset+4]
		weight := float64(pixel[3]) * contribution.weight
		red += float64(pixel[0]) * weight
		green += float64(pixel[1]) * weight
		blue += float64(pixel[2]) * weight
		alpha += weight
	}
	if alpha == 0 {
		return color.NRGBA{}, nil
	}
	inverse := 1 / alpha
	return color.NRGBA{roundModelChannel(red * inverse), roundModelChannel(green * inverse), roundModelChannel(blue * inverse), roundModelChannel(alpha)}, nil
}

func roundModelChannel(value float64) uint8 {
	return uint8(min(255, max(0, int64(value+0.5))))
}

func scanModelImageLine(ctx context.Context, img image.Image, line int, horizontal bool, pixels []byte, palette []color.NRGBA) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	bounds := img.Bounds()
	if raw, ok := img.(*image.NRGBA); ok {
		if horizontal {
			offset := raw.PixOffset(bounds.Min.X, bounds.Min.Y+line)
			copy(pixels, raw.Pix[offset:offset+len(pixels)])
			return ctx.Err()
		}
		for index, offset := 0, line*4; index < len(pixels); index, offset = index+4, offset+raw.Stride {
			if index&1023 == 0 && ctx.Err() != nil {
				return ctx.Err()
			}
			copy(pixels[index:index+4], raw.Pix[offset:offset+4])
		}
		return ctx.Err()
	}
	if raw, ok := img.(*image.YCbCr); ok {
		return scanModelYCbCrLine(ctx, raw, line, horizontal, pixels)
	}
	for index := 0; index < len(pixels)/4; index++ {
		if index&255 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		x, y := line, index
		if horizontal {
			x, y = index, line
		}
		pixel := modelImagePixel(img, x+bounds.Min.X, y+bounds.Min.Y, palette)
		copy(pixels[index*4:index*4+4], []byte{pixel.R, pixel.G, pixel.B, pixel.A})
	}
	return ctx.Err()
}

func scanModelYCbCrLine(ctx context.Context, img *image.YCbCr, line int, horizontal bool, pixels []byte) error {
	x, y := img.Rect.Min.X+line, img.Rect.Min.Y
	stepX, stepY, stepLuma := 0, 1, img.YStride
	if horizontal {
		x, y = img.Rect.Min.X, img.Rect.Min.Y+line
		stepX, stepY, stepLuma = 1, 0, 1
	}
	for index, luma := 0, img.YOffset(x, y); index < len(pixels); index, luma = index+4, luma+stepLuma {
		if index&1023 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		chroma := img.COffset(x, y)
		red, green, blue := color.YCbCrToRGB(img.Y[luma], img.Cb[chroma], img.Cr[chroma])
		destination := pixels[index : index+4 : index+4]
		destination[0], destination[1], destination[2], destination[3] = red, green, blue, 255
		x, y = x+stepX, y+stepY
	}
	return ctx.Err()
}

func modelImagePixel(img image.Image, x, y int, palette []color.NRGBA) color.NRGBA {
	switch source := img.(type) {
	case *image.NRGBA:
		return source.NRGBAAt(x, y)
	case *image.NRGBA64:
		pixel := source.NRGBA64At(x, y)
		return color.NRGBA{uint8(pixel.R >> 8), uint8(pixel.G >> 8), uint8(pixel.B >> 8), uint8(pixel.A >> 8)}
	case *image.RGBA:
		pixel := source.RGBAAt(x, y)
		if pixel.A == 0 {
			return color.NRGBA{}
		}
		if pixel.A == 255 {
			return color.NRGBA{pixel.R, pixel.G, pixel.B, pixel.A}
		}
		return color.NRGBA{uint8(uint16(pixel.R) * 255 / uint16(pixel.A)), uint8(uint16(pixel.G) * 255 / uint16(pixel.A)), uint8(uint16(pixel.B) * 255 / uint16(pixel.A)), pixel.A}
	case *image.RGBA64:
		pixel := source.RGBA64At(x, y)
		alpha := uint8(pixel.A >> 8)
		if alpha == 0 {
			return color.NRGBA{}
		}
		if alpha == 255 {
			return color.NRGBA{uint8(pixel.R >> 8), uint8(pixel.G >> 8), uint8(pixel.B >> 8), alpha}
		}
		return color.NRGBA{uint8((uint32(pixel.R) * 65535 / uint32(pixel.A)) >> 8), uint8((uint32(pixel.G) * 65535 / uint32(pixel.A)) >> 8), uint8((uint32(pixel.B) * 65535 / uint32(pixel.A)) >> 8), alpha}
	case *image.YCbCr:
		pixel := source.YCbCrAt(x, y)
		red, green, blue := color.YCbCrToRGB(pixel.Y, pixel.Cb, pixel.Cr)
		return color.NRGBA{red, green, blue, 255}
	case *image.Gray:
		value := source.GrayAt(x, y).Y
		return color.NRGBA{value, value, value, 255}
	case *image.Gray16:
		value := uint8(source.Gray16At(x, y).Y >> 8)
		return color.NRGBA{value, value, value, 255}
	case *image.Paletted:
		return palette[source.Pix[source.PixOffset(x, y)]]
	default:
		red, green, blue, alpha := img.At(x, y).RGBA()
		if alpha == 0 {
			return color.NRGBA{}
		}
		return color.NRGBA{uint8((red * 65535 / alpha) >> 8), uint8((green * 65535 / alpha) >> 8), uint8((blue * 65535 / alpha) >> 8), uint8(alpha >> 8)}
	}
}
