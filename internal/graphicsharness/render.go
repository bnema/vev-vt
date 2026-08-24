// Package graphicsharness provides a headless reference compositor for
// inspecting graphics snapshots during development. It is intentionally
// internal: vev-vt remains renderer-neutral in its public API.
package graphicsharness

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/bits"

	"github.com/bnema/vev-vt/graphics"
)

const maxCanvasPixels = uint64(64 << 20)

// Render composites a graphics snapshot into an RGBA frame covering viewport.
// Placement pixels are sampled with nearest-neighbor scaling in layer and
// insertion order. The viewport's origin is preserved for placement geometry
// but maps to (0, 0) in the returned frame.
func Render(snapshot *graphics.Snapshot, viewport graphics.PixelRect) (*image.RGBA, error) {
	if !viewport.Valid() || viewport.Empty() {
		return nil, fmt.Errorf("render graphics: invalid viewport")
	}
	width, height, ok := imageDimensions(viewport.Width, viewport.Height)
	if !ok {
		return nil, fmt.Errorf("render graphics: viewport exceeds %d pixels", maxCanvasPixels)
	}
	frame := image.NewRGBA(image.Rect(0, 0, width, height))
	if snapshot == nil {
		return frame, nil
	}
	decoded := make(map[graphics.AssetID]*image.RGBA)
	for _, placement := range snapshot.Placements() {
		asset, ok := snapshot.Asset(placement.AssetID())
		if !ok {
			return nil, fmt.Errorf("render graphics: placement %s references an unavailable asset", placement.ID())
		}
		source, ok := decoded[asset.ID()]
		if !ok {
			var err error
			source, err = decodeAsset(asset)
			if err != nil {
				return nil, err
			}
			decoded[asset.ID()] = source
		}
		if err := composite(frame, viewport, source, placement.Source(), placement.Destination()); err != nil {
			return nil, fmt.Errorf("render graphics: placement %s: %w", placement.ID(), err)
		}
	}
	return frame, nil
}

func imageDimensions(width, height int64) (int, int, bool) {
	if width <= 0 || height <= 0 || uint64(width) > uint64(maxInt()) || uint64(height) > uint64(maxInt()) {
		return 0, 0, false
	}
	overflow, pixels := bits.Mul64(uint64(width), uint64(height))
	if overflow != 0 || pixels > maxCanvasPixels {
		return 0, 0, false
	}
	return int(width), int(height), true
}

func maxInt() int { return int(^uint(0) >> 1) }

func decodeAsset(asset graphics.AssetView) (*image.RGBA, error) {
	width, height, ok := imageDimensions(asset.Width(), asset.Height())
	if !ok {
		return nil, fmt.Errorf("decode asset %s: invalid dimensions", asset.ID())
	}
	encoded := asset.Encoded()
	switch asset.Format() {
	case graphics.AssetFormatPNG:
		decoded, err := png.Decode(bytes.NewReader(encoded))
		if err != nil {
			return nil, fmt.Errorf("decode asset %s: %w", asset.ID(), err)
		}
		if decoded.Bounds().Dx() != width || decoded.Bounds().Dy() != height {
			return nil, fmt.Errorf("decode asset %s: PNG dimensions do not match snapshot", asset.ID())
		}
		result := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := range height {
			for x := range width {
				result.Set(x, y, decoded.At(decoded.Bounds().Min.X+x, decoded.Bounds().Min.Y+y))
			}
		}
		return result, nil
	case graphics.AssetFormatRGB, graphics.AssetFormatRGBA:
		channels := 3
		if asset.Format() == graphics.AssetFormatRGBA {
			channels = 4
		}
		expected := uint64(width) * uint64(height) * uint64(channels)
		if uint64(len(encoded)) != expected {
			return nil, fmt.Errorf("decode asset %s: raw payload has %d bytes, want %d", asset.ID(), len(encoded), expected)
		}
		result := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := range height {
			for x := range width {
				offset := (y*width + x) * channels
				r, g, b, a := encoded[offset], encoded[offset+1], encoded[offset+2], byte(255)
				if channels == 4 {
					a = encoded[offset+3]
				}
				result.SetRGBA(x, y, color.RGBA{R: premultiply(r, a), G: premultiply(g, a), B: premultiply(b, a), A: a})
			}
		}
		return result, nil
	default:
		return nil, fmt.Errorf("decode asset %s: unsupported format %d", asset.ID(), asset.Format())
	}
}

func composite(frame *image.RGBA, viewport graphics.PixelRect, source *image.RGBA, sourceRect, destination graphics.PixelRect) error {
	visible, ok := destination.Intersect(viewport)
	if !ok {
		return nil
	}
	for y := visible.Y; y < visible.Y+visible.Height; y++ {
		sy, ok := scaledCoordinate(y, destination.Y, destination.Height, sourceRect.Y, sourceRect.Height)
		if !ok {
			return fmt.Errorf("invalid vertical mapping")
		}
		for x := visible.X; x < visible.X+visible.Width; x++ {
			sx, ok := scaledCoordinate(x, destination.X, destination.Width, sourceRect.X, sourceRect.Width)
			if !ok || sx < 0 || sy < 0 || sx >= int64(source.Bounds().Dx()) || sy >= int64(source.Bounds().Dy()) {
				return fmt.Errorf("invalid source mapping")
			}
			dx, dy := int(x-viewport.X), int(y-viewport.Y)
			blend(frame, dx, dy, source.RGBAAt(int(sx), int(sy)))
		}
	}
	return nil
}

func scaledCoordinate(value, destinationStart, destinationSize, sourceStart, sourceSize int64) (int64, bool) {
	if destinationSize <= 0 || sourceSize <= 0 || value < destinationStart {
		return 0, false
	}
	offset := value - destinationStart
	if offset >= destinationSize {
		return 0, false
	}
	hi, lo := bits.Mul64(uint64(offset), uint64(sourceSize))
	if hi >= uint64(destinationSize) {
		return 0, false
	}
	quotient, _ := bits.Div64(hi, lo, uint64(destinationSize))
	if quotient > uint64(^uint64(0)>>1) || sourceStart > int64(^uint64(0)>>1)-int64(quotient) {
		return 0, false
	}
	return sourceStart + int64(quotient), true
}

func premultiply(component, alpha byte) byte {
	return byte((uint16(component)*uint16(alpha) + 127) / 255)
}

func blend(dst *image.RGBA, x, y int, src color.RGBA) {
	offset := dst.PixOffset(x, y)
	inverse := uint16(255 - src.A)
	dst.Pix[offset] = src.R + byte((uint16(dst.Pix[offset])*inverse+127)/255)
	dst.Pix[offset+1] = src.G + byte((uint16(dst.Pix[offset+1])*inverse+127)/255)
	dst.Pix[offset+2] = src.B + byte((uint16(dst.Pix[offset+2])*inverse+127)/255)
	dst.Pix[offset+3] = src.A + byte((uint16(dst.Pix[offset+3])*inverse+127)/255)
}
