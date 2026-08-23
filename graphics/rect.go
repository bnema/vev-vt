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

type rect struct {
	x, y          int64
	width, height int64
}

func pixelRect(r PixelRect) rect { return rect{x: r.X, y: r.Y, width: r.Width, height: r.Height} }
func cellRect(r CellRect) rect   { return rect{x: r.X, y: r.Y, width: r.Width, height: r.Height} }

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

func (r rect) valid() bool {
	return r.width >= 0 && r.height >= 0 && edge(r.x, r.width) && edge(r.y, r.height)
}

func (r rect) empty() bool { return r.width == 0 || r.height == 0 }

func (r rect) right() (int64, bool) { return checkedEdge(r.x, r.width) }

func (r rect) bottom() (int64, bool) { return checkedEdge(r.y, r.height) }

func (r rect) contains(x, y int64) bool {
	right, okRight := r.right()
	bottom, okBottom := r.bottom()
	return okRight && okBottom && x >= r.x && x < right && y >= r.y && y < bottom
}

func (r rect) intersect(other rect) (rect, bool) {
	if !r.valid() || !other.valid() || r.empty() || other.empty() {
		return rect{}, false
	}
	right, _ := r.right()
	otherRight, _ := other.right()
	bottom, _ := r.bottom()
	otherBottom, _ := other.bottom()
	left := max(r.x, other.x)
	top := max(r.y, other.y)
	right = min(right, otherRight)
	bottom = min(bottom, otherBottom)
	if right <= left || bottom <= top {
		return rect{}, false
	}
	return rect{x: left, y: top, width: right - left, height: bottom - top}, true
}

// Valid reports whether the rectangle's dimensions and exclusive edges are
// representable without signed overflow.
func (r PixelRect) Valid() bool { return pixelRect(r).valid() }

// Valid reports whether the rectangle's dimensions and exclusive edges are
// representable without signed overflow.
func (r CellRect) Valid() bool { return cellRect(r).valid() }

// Empty reports whether the rectangle has no area.
func (r PixelRect) Empty() bool { return pixelRect(r).empty() }

// Empty reports whether the rectangle has no area.
func (r CellRect) Empty() bool { return cellRect(r).empty() }

// Right returns the exclusive right edge. The boolean is false for an
// invalid rectangle; it is never produced by arithmetic that wraps.
func (r PixelRect) Right() (int64, bool) { return pixelRect(r).right() }

// Bottom returns the exclusive bottom edge. The boolean is false for an
// invalid rectangle; it is never produced by arithmetic that wraps.
func (r PixelRect) Bottom() (int64, bool) { return pixelRect(r).bottom() }

// Right returns the exclusive right edge. The boolean is false for an
// invalid rectangle; it is never produced by arithmetic that wraps.
func (r CellRect) Right() (int64, bool) { return cellRect(r).right() }

// Bottom returns the exclusive bottom edge. The boolean is false for an
// invalid rectangle; it is never produced by arithmetic that wraps.
func (r CellRect) Bottom() (int64, bool) { return cellRect(r).bottom() }

// Contains reports whether a point is inside the half-open rectangle.
func (r PixelRect) Contains(x, y int64) bool { return pixelRect(r).contains(x, y) }

// Contains reports whether a point is inside the half-open rectangle.
func (r CellRect) Contains(x, y int64) bool { return cellRect(r).contains(x, y) }

// Intersect returns the non-empty intersection of two rectangles. Invalid or
// empty rectangles do not intersect. The result is safe even when the input
// edges are close to the int64 limits.
func (r PixelRect) Intersect(other PixelRect) (PixelRect, bool) {
	intersect, ok := pixelRect(r).intersect(pixelRect(other))
	return PixelRect{X: intersect.x, Y: intersect.y, Width: intersect.width, Height: intersect.height}, ok
}

// Intersect returns the non-empty intersection of two rectangles.
func (r CellRect) Intersect(other CellRect) (CellRect, bool) {
	intersect, ok := cellRect(r).intersect(cellRect(other))
	return CellRect{X: intersect.x, Y: intersect.y, Width: intersect.width, Height: intersect.height}, ok
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
