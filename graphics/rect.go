package graphics

import "fmt"

const (
	maxInt64 = int64(^uint64(0) >> 1)
	minInt64 = -maxInt64 - 1
)

// PixelRect is a half-open rectangle in pixel coordinates. Width and Height
// must be non-negative, and the exclusive right and bottom edges must fit in
// int64. Coordinates may be negative so that scenes can be clipped without
// first translating them into a viewport.
type PixelRect struct {
	X, Y          int64
	Width, Height int64
}

// CellRect is a half-open rectangle in cell coordinates. It has the same
// overflow rules as PixelRect but is kept as a distinct type so that pixel
// and cell coordinates cannot be accidentally mixed.
type CellRect struct {
	X, Y          int64
	Width, Height int64
}

// NewPixelRect constructs a checked pixel rectangle.
func NewPixelRect(x, y, width, height int64) (PixelRect, error) {
	r := PixelRect{X: x, Y: y, Width: width, Height: height}
	if !r.Valid() {
		return PixelRect{}, fmt.Errorf("pixel rectangle: %w", ErrInvalidRect)
	}
	return r, nil
}

// NewCellRect constructs a checked cell rectangle.
func NewCellRect(x, y, width, height int64) (CellRect, error) {
	r := CellRect{X: x, Y: y, Width: width, Height: height}
	if !r.Valid() {
		return CellRect{}, fmt.Errorf("cell rectangle: %w", ErrInvalidRect)
	}
	return r, nil
}

// Valid reports whether the rectangle's dimensions and exclusive edges are
// representable without signed overflow.
func (r PixelRect) Valid() bool {
	return r.Width >= 0 && r.Height >= 0 && edge(r.X, r.Width) && edge(r.Y, r.Height)
}

// Valid reports whether the rectangle's dimensions and exclusive edges are
// representable without signed overflow.
func (r CellRect) Valid() bool {
	return r.Width >= 0 && r.Height >= 0 && edge(r.X, r.Width) && edge(r.Y, r.Height)
}

// Empty reports whether the rectangle has no area.
func (r PixelRect) Empty() bool { return r.Width == 0 || r.Height == 0 }

// Empty reports whether the rectangle has no area.
func (r CellRect) Empty() bool { return r.Width == 0 || r.Height == 0 }

// Right returns the exclusive right edge. The boolean is false for an
// invalid rectangle; it is never produced by arithmetic that wraps.
func (r PixelRect) Right() (int64, bool) { return checkedEdge(r.X, r.Width) }

// Bottom returns the exclusive bottom edge. The boolean is false for an
// invalid rectangle; it is never produced by arithmetic that wraps.
func (r PixelRect) Bottom() (int64, bool) { return checkedEdge(r.Y, r.Height) }

// Right returns the exclusive right edge. The boolean is false for an
// invalid rectangle; it is never produced by arithmetic that wraps.
func (r CellRect) Right() (int64, bool) { return checkedEdge(r.X, r.Width) }

// Bottom returns the exclusive bottom edge. The boolean is false for an
// invalid rectangle; it is never produced by arithmetic that wraps.
func (r CellRect) Bottom() (int64, bool) { return checkedEdge(r.Y, r.Height) }

// Contains reports whether a point is inside the half-open rectangle.
func (r PixelRect) Contains(x, y int64) bool {
	right, okRight := r.Right()
	bottom, okBottom := r.Bottom()
	return okRight && okBottom && x >= r.X && x < right && y >= r.Y && y < bottom
}

// Contains reports whether a point is inside the half-open rectangle.
func (r CellRect) Contains(x, y int64) bool {
	right, okRight := r.Right()
	bottom, okBottom := r.Bottom()
	return okRight && okBottom && x >= r.X && x < right && y >= r.Y && y < bottom
}

// Intersect returns the non-empty intersection of two rectangles. Invalid or
// empty rectangles do not intersect. The result is safe even when the input
// edges are close to the int64 limits.
func (r PixelRect) Intersect(other PixelRect) (PixelRect, bool) {
	if !r.Valid() || !other.Valid() || r.Empty() || other.Empty() {
		return PixelRect{}, false
	}
	right, _ := r.Right()
	otherRight, _ := other.Right()
	bottom, _ := r.Bottom()
	otherBottom, _ := other.Bottom()
	left := max(r.X, other.X)
	top := max(r.Y, other.Y)
	right = min(right, otherRight)
	bottom = min(bottom, otherBottom)
	if right <= left || bottom <= top {
		return PixelRect{}, false
	}
	return PixelRect{X: left, Y: top, Width: right - left, Height: bottom - top}, true
}

// Intersect returns the non-empty intersection of two rectangles.
func (r CellRect) Intersect(other CellRect) (CellRect, bool) {
	if !r.Valid() || !other.Valid() || r.Empty() || other.Empty() {
		return CellRect{}, false
	}
	right, _ := r.Right()
	otherRight, _ := other.Right()
	bottom, _ := r.Bottom()
	otherBottom, _ := other.Bottom()
	left := max(r.X, other.X)
	top := max(r.Y, other.Y)
	right = min(right, otherRight)
	bottom = min(bottom, otherBottom)
	if right <= left || bottom <= top {
		return CellRect{}, false
	}
	return CellRect{X: left, Y: top, Width: right - left, Height: bottom - top}, true
}

func edge(start, length int64) bool {
	_, ok := checkedEdge(start, length)
	return ok
}

func checkedEdge(start, length int64) (int64, bool) {
	if length < 0 {
		return 0, false
	}
	// For a non-negative length, a negative start cannot overflow the
	// positive edge: even MinInt64 + MaxInt64 is -1. Avoid computing
	// maxInt64-start in that case because the subtraction itself could wrap.
	if start >= 0 && length > maxInt64-start {
		return 0, false
	}
	return start + length, true
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
