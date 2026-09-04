package core

import "fmt"

// CellSource exposes semantic terminal cells without exposing mutable storage.
// Implementations may use any physical layout; callers must use Cell for reads.
type CellSource interface {
	Columns() int
	Rows() int
	Cell(x, y int) Cell
}

// Frame is a fixed-size grid of cells. Logical rows are decoupled from their
// physical storage through lineOffset: logical row y lives in the private
// cell slab selected by lineOffset[y]. This indirection lets full-width
// scrolls rotate offsets instead of copying cell memory (NeoVim's grid
// technique; cf. nvim grid.c line_offset[]). All cell access must go through
// the accessors (At/Set/Row) so callers never assume a physical layout.
type Frame struct {
	Width  int
	Height int
	cells  []Cell
	// lineOffset maps a logical row index to its physical base index into
	// cells. It is always a permutation of the canonical offsets y*Width.
	lineOffset []int
}

func NewFrame(width, height int) Frame {
	cells := make([]Cell, width*height)
	for i := range cells {
		cells[i] = BlankCell()
	}
	lineOffset := make([]int, height)
	for y := range lineOffset {
		lineOffset[y] = y * width
	}
	return Frame{Width: width, Height: height, cells: cells, lineOffset: lineOffset}
}

// Clone returns an independent copy preserving logical row contents. The clone
// uses canonical row storage so later scrolls or writes to the source frame do
// not affect it.
func (f Frame) Clone() Frame {
	clone := NewFrame(f.Width, f.Height)
	for y := range f.Height {
		copy(clone.row(y), f.row(y))
	}
	return clone
}

// Replace copies src into f while reusing canonical storage when dimensions
// permit. src is read through logical rows so its physical row rotation is
// preserved in the resulting contents without exposing layout internals.
func (f *Frame) Replace(src Frame) {
	if f == nil {
		return
	}
	if src.Width <= 0 || src.Height <= 0 || len(src.cells) != src.Width*src.Height {
		*f = Frame{}
		return
	}

	sameDimensions := f.Width == src.Width && f.Height == src.Height
	cellCount := src.Width * src.Height
	if sameDimensions && cap(f.cells) >= cellCount {
		f.cells = f.cells[:cellCount]
	} else {
		f.cells = make([]Cell, cellCount)
	}
	if sameDimensions && cap(f.lineOffset) >= src.Height {
		f.lineOffset = f.lineOffset[:src.Height]
	} else {
		f.lineOffset = make([]int, src.Height)
	}
	f.Width = src.Width
	f.Height = src.Height
	for y := range f.lineOffset {
		f.lineOffset[y] = y * f.Width
	}
	for y := range src.Height {
		copy(f.row(y), src.row(y))
	}
}

func (f Frame) Validate() error {
	if f.Width <= 0 || f.Height <= 0 {
		return fmt.Errorf("invalid frame size %dx%d", f.Width, f.Height)
	}
	if len(f.cells) != f.Width*f.Height {
		return fmt.Errorf("invalid cell count: got %d want %d", len(f.cells), f.Width*f.Height)
	}
	if len(f.lineOffset) != f.Height {
		return fmt.Errorf("invalid lineOffset length: got %d want %d", len(f.lineOffset), f.Height)
	}
	return nil
}

// CheckInvariants verifies that lineOffset is a permutation of the canonical
// row offsets: every entry is a non-negative multiple of Width within bounds,
// and every logical row maps to a distinct physical row. It is used by tests
// and assertions; the hot path uses the lighter Validate.
func (f Frame) CheckInvariants() error {
	if f.Width <= 0 {
		return fmt.Errorf("invalid width %d", f.Width)
	}
	if len(f.lineOffset) != f.Height {
		return fmt.Errorf("lineOffset length %d != height %d", len(f.lineOffset), f.Height)
	}
	seen := make([]bool, f.Height)
	for y, off := range f.lineOffset {
		if off < 0 || off%f.Width != 0 {
			return fmt.Errorf("row %d: offset %d is not a non-negative multiple of width %d", y, off, f.Width)
		}
		phys := off / f.Width
		if phys >= f.Height {
			return fmt.Errorf("row %d: offset %d maps to physical row %d out of range [0,%d)", y, off, phys, f.Height)
		}
		if seen[phys] {
			return fmt.Errorf("row %d: physical row %d already mapped by another logical row", y, phys)
		}
		seen[phys] = true
	}
	return nil
}

// Columns returns the frame width.
func (f Frame) Columns() int { return f.Width }

// Rows returns the frame height.
func (f Frame) Rows() int { return f.Height }

// Cell returns the semantic cell at x, y.
func (f Frame) Cell(x, y int) Cell { return f.At(x, y) }

func (f Frame) At(x, y int) Cell {
	return f.cells[f.lineOffset[y]+x]
}

func (f Frame) Set(x, y int, cell Cell) {
	f.cells[f.lineOffset[y]+x] = cell
}

// Row returns an owned copy of logical row y.
func (f Frame) Row(y int) []Cell {
	row := make([]Cell, f.Width)
	copy(row, f.row(y))
	return row
}

// WriteRow copies cells into logical row y starting at x.
func (f Frame) WriteRow(y, x int, cells []Cell) int {
	return copy(f.row(y)[x:], cells)
}

// CopyRow moves count cells within logical row y. Overlapping ranges are safe.
func (f Frame) CopyRow(y, dst, src, count int) {
	row := f.row(y)
	copy(row[dst:dst+count], row[src:src+count])
}

// FillRow writes cell into [start, end) of logical row y.
func (f Frame) FillRow(y, start, end int, cell Cell) {
	row := f.row(y)
	for x := start; x < end; x++ {
		row[x] = cell
	}
}

func (f Frame) row(y int) []Cell {
	base := f.lineOffset[y]
	return f.cells[base : base+f.Width]
}

// ScrollUp scrolls the logical rows in [top,bottom] up by n lines by rotating
// their physical offsets: no per-cell copying. Rows scrolled off the top are
// recycled to the bottom and blanked in place. It assumes the full frame width
// and 0 <= top <= bottom < Height and 0 < n <= bottom-top+1 (the caller
// clamps). The receiver is a value, but lineOffset and cells alias the caller's
// backing arrays, so in-place mutations are visible to the caller.
func (f Frame) ScrollUp(top, bottom, n int) {
	for ; n > 0; n-- {
		recycled := f.lineOffset[top]
		for y := top; y < bottom; y++ {
			f.lineOffset[y] = f.lineOffset[y+1]
		}
		f.lineOffset[bottom] = recycled
		blankRow(f.cells[recycled : recycled+f.Width])
	}
}

// ScrollDown scrolls the logical rows in [top,bottom] down by n lines by
// rotating their physical offsets. Rows scrolled off the bottom are recycled
// to the top and blanked in place. See ScrollUp for preconditions.
func (f Frame) ScrollDown(top, bottom, n int) {
	for ; n > 0; n-- {
		recycled := f.lineOffset[bottom]
		for y := bottom; y > top; y-- {
			f.lineOffset[y] = f.lineOffset[y-1]
		}
		f.lineOffset[top] = recycled
		blankRow(f.cells[recycled : recycled+f.Width])
	}
}

func blankRow(row []Cell) {
	for i := range row {
		row[i] = BlankCell()
	}
}
