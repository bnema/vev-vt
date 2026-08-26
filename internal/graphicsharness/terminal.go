package graphicsharness

import (
	"fmt"
	"image"

	vt "github.com/bnema/vev-vt"
	"github.com/bnema/vev-vt/graphics"
)

// RenderTerminal feeds terminal bytes into a fresh screen and composites its
// active graphics snapshot using geometry's pixel viewport.
func RenderTerminal(input []byte, geometry vt.Geometry) (*image.RGBA, error) {
	if !geometry.Valid() || !geometry.PixelsKnown() {
		return nil, fmt.Errorf("render terminal: complete cell and pixel geometry is required")
	}
	screen := vt.NewScreen(geometry.Cols, geometry.Rows)
	screen.SetGeometry(geometry)
	screen.Write(input)
	return Render(screen.GraphicsSnapshot(), graphics.PixelRect{Width: int64(geometry.PixelWidth), Height: int64(geometry.PixelHeight)})
}
