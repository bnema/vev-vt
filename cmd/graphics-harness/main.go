// Command graphics-harness renders Kitty graphics terminal input to a PNG for
// local development inspection. It is not a terminal emulator.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"

	vt "github.com/bnema/vev-vt"
	"github.com/bnema/vev-vt/internal/graphicsharness"
)

func main() {
	inputPath := flag.String("input", "", "path to a file containing raw terminal bytes")
	outputPath := flag.String("output", "graphics-harness.png", "path for the rendered PNG")
	cols := flag.Int("cols", 80, "terminal column count")
	rows := flag.Int("rows", 24, "terminal row count")
	pixelWidth := flag.Int("pixel-width", 640, "terminal viewport width in pixels")
	pixelHeight := flag.Int("pixel-height", 384, "terminal viewport height in pixels")
	scale := flag.Int("scale", 1, "nearest-neighbor output scale for inspection")
	flag.Parse()

	if *inputPath == "" {
		fail("-input is required")
	}
	input, err := os.ReadFile(*inputPath)
	if err != nil {
		fail("read input: %v", err)
	}
	frame, err := graphicsharness.RenderTerminal(input, vt.Geometry{
		Cols: *cols, Rows: *rows, PixelWidth: *pixelWidth, PixelHeight: *pixelHeight,
	})
	if err != nil {
		fail("render input: %v", err)
	}
	frame, err = scaleFrame(frame, *scale)
	if err != nil {
		fail("scale output: %v", err)
	}
	output, err := os.Create(*outputPath)
	if err != nil {
		fail("create output: %v", err)
	}
	if err := png.Encode(output, frame); err != nil {
		_ = output.Close()
		fail("encode output: %v", err)
	}
	if err := output.Close(); err != nil {
		fail("close output: %v", err)
	}
}

func scaleFrame(source *image.RGBA, scale int) (*image.RGBA, error) {
	if scale < 1 {
		return nil, fmt.Errorf("scale must be positive")
	}
	width, height := source.Bounds().Dx(), source.Bounds().Dy()
	maxInt := int(^uint(0) >> 1)
	if width > 0 && scale > maxInt/width {
		return nil, fmt.Errorf("scaled frame exceeds 67108864 pixels")
	}
	if height > 0 && scale > maxInt/height {
		return nil, fmt.Errorf("scaled frame exceeds 67108864 pixels")
	}
	scaledWidth, scaledHeight := width*scale, height*scale
	const maxScaledPixels = uint64(64 << 20)
	if scaledWidth > 0 && uint64(scaledHeight) > maxScaledPixels/uint64(scaledWidth) {
		return nil, fmt.Errorf("scaled frame exceeds 67108864 pixels")
	}
	result := image.NewRGBA(image.Rect(0, 0, scaledWidth, scaledHeight))
	for y := range height {
		for x := range width {
			pixel := source.RGBAAt(x, y)
			for dy := range scale {
				for dx := range scale {
					result.SetRGBA(x*scale+dx, y*scale+dy, pixel)
				}
			}
		}
	}
	return result, nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "graphics-harness: "+format+"\n", args...)
	os.Exit(1)
}
