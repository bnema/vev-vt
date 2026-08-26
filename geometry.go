package vt

// Geometry combines a terminal's required cell dimensions with optional pixel
// dimensions reported by its frontend. Zero pixel dimensions mean they are
// unknown and must not be inferred from the cell dimensions.
type Geometry struct {
	Cols, Rows              int
	PixelWidth, PixelHeight int
}

// Valid reports whether the cell dimensions can describe a terminal screen.
func (g Geometry) Valid() bool {
	return g.Cols > 0 && g.Rows > 0
}

// PixelsKnown reports whether both pixel dimensions are available.
func (g Geometry) PixelsKnown() bool {
	return g.PixelWidth > 0 && g.PixelHeight > 0
}
