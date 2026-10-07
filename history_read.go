package vt

import renderer "github.com/bnema/vev-vt/core"

// RowWidth returns the retained width of row y, or zero outside the view.
// Unlike Row, it does not allocate or decode a semantic row.
func (v HistoryView) RowWidth(y int) int {
	chunk, _ := v.locateRow(y)
	if chunk == nil {
		return 0
	}
	return chunk.width
}

// Cell returns one semantic cell without allocating a row. Coordinates follow
// Screen.Cell: x is the column and y is the row. Invalid coordinates return the
// canonical blank cell. Variable-width history does not implement CellSource.
func (v HistoryView) Cell(x, y int) renderer.Cell {
	chunk, row := v.locateRow(y)
	if chunk == nil || x < 0 || x >= chunk.width {
		return renderer.BlankCell()
	}
	return chunk.cell(row, x)
}

// CopyRow copies up to len(dst) cells into caller-owned storage and returns the
// number copied. It neither allocates a row nor exposes mutable page storage.
func (v HistoryView) CopyRow(y int, dst []renderer.Cell) int {
	chunk, row := v.locateRow(y)
	if chunk == nil {
		return 0
	}
	n := min(len(dst), chunk.width)
	frame := chunk.frameView()
	for x := range n {
		dst[x] = frame.Cell(x, chunk.start+row)
	}
	return n
}

// RangeText streams each row's text in order without allocating per row. For
// row i of the given cell width, runes holds one rune per non-continuation
// cell (zero runes read as a space) and columns holds the cell column of each
// rune. Both slices are only valid during the callback. Like Range, it
// restores at most one cold page at a time and does not populate the page
// cache.
func (v HistoryView) RangeText(yield func(i int, runes []rune, columns []int, width int) bool) error {
	var runes []rune
	var columns []int
	i := 0
	for _, chunk := range v.chunks {
		frame, err := chunk.page.readFrame(false)
		if err != nil {
			return err
		}
		for row := range chunk.len() {
			runes, columns = frame.AppendRowText(runes[:0], columns[:0], chunk.start+row)
			if !yield(i, runes, columns, chunk.width) {
				return nil
			}
			i++
		}
	}
	return nil
}

func (v HistoryView) locateRow(y int) (*HistoryChunk, int) {
	if y < 0 {
		return nil, 0
	}
	for _, chunk := range v.chunks {
		if y < chunk.len() {
			return chunk, y
		}
		y -= chunk.len()
	}
	return nil, 0
}
