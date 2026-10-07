package core

import (
	"fmt"
	"maps"
	"math"
)

// CellSource exposes semantic terminal cells without exposing mutable storage.
// Implementations may use any physical layout; callers must use Cell for reads.
type CellSource interface {
	Columns() int
	Rows() int
	Cell(x, y int) Cell
}

const (
	// StoredCellLogicalBytes is the deterministic uncompressed size of one
	// compact cell, independent of the Go allocator and host architecture.
	StoredCellLogicalBytes uint64 = 16
	// RowDescriptorLogicalBytes is the deterministic size of one compact row
	// descriptor.
	RowDescriptorLogicalBytes uint64 = 4
	// StyleRecordLogicalBytes is the deterministic size of one canonical
	// page-local style record.
	StyleRecordLogicalBytes uint64 = 32

	continuationFlag = uint8(1)
)

type storedCell struct {
	rune      int32
	styleID   uint32
	payloadID uint32
	flags     uint8
}

type styleSlot struct {
	style Style
	refs  uint32
	used  bool
}

type cellPage struct {
	cells      []storedCell
	rows       []uint32
	styles     []styleSlot
	styleIndex map[Style]uint32
	freeStyles []uint32
	styleCount uint32
	// styleCache remembers the most recent raw Style passed to internStyle and
	// the live slot it resolved to. It is a pure accelerator: it is cleared
	// whenever that slot is freed, and a fresh page starts with it invalid.
	// Clone copies it along with the style slots it refers to, so a clone stays
	// reflect.DeepEqual to its source.
	styleCache   Style
	styleCacheID uint32
	styleCacheOK bool
	payloads     []payloadSlot
	payloadIndex map[CellPayload]uint32
	freePayloads []uint32
	payloadBytes uint64
}

// Frame is a fixed-size semantic grid backed by one compact page. Cells carry
// page-local style IDs; callers never observe those IDs or the physical row
// layout. Logical rows are mapped through compact row descriptors so full-width
// scrolling rotates descriptors instead of copying cells.
type Frame struct {
	Width  int
	Height int
	page   *cellPage
}

func NewFrame(width, height int) Frame {
	cellCount, ok := frameCellCount(width, height)
	if !ok {
		panic("frame cell count exceeds page limit")
	}
	page := &cellPage{
		cells:      make([]storedCell, cellCount),
		rows:       make([]uint32, height),
		styles:     []styleSlot{{style: DefaultStyle(), refs: uint32(cellCount), used: true}},
		styleIndex: map[Style]uint32{DefaultStyle(): 0},
		styleCount: 1,
	}
	for i := range page.cells {
		page.cells[i].rune = ' '
	}
	for y := range page.rows {
		page.rows[y] = uint32(y * width)
	}
	return Frame{Width: width, Height: height, page: page}
}

// Clone returns an independent compact page preserving physical row rotation
// and page-local style IDs.
func (f Frame) Clone() Frame {
	if f.page == nil {
		return Frame{Width: f.Width, Height: f.Height}
	}
	return Frame{
		Width:  f.Width,
		Height: f.Height,
		page: &cellPage{
			cells:        append([]storedCell(nil), f.page.cells...),
			rows:         append([]uint32(nil), f.page.rows...),
			styles:       append([]styleSlot(nil), f.page.styles...),
			styleIndex:   maps.Clone(f.page.styleIndex),
			freeStyles:   append([]uint32(nil), f.page.freeStyles...),
			styleCount:   f.page.styleCount,
			styleCache:   f.page.styleCache,
			styleCacheID: f.page.styleCacheID,
			styleCacheOK: f.page.styleCacheOK,
			payloads:     append([]payloadSlot(nil), f.page.payloads...),
			payloadIndex: maps.Clone(f.page.payloadIndex),
			freePayloads: append([]uint32(nil), f.page.freePayloads...),
			payloadBytes: f.page.payloadBytes,
		},
	}
}

// Replace replaces f with an independent structural copy of src's compact page.
// An invalid or empty source leaves the destination unchanged.
func (f *Frame) Replace(src Frame) {
	if f == nil {
		return
	}
	if err := src.validateStorage(); err != nil || src.Width <= 0 || src.Height <= 0 {
		return
	}
	*f = src.Clone()
}

func frameCellCount(width, height int) (int, bool) {
	if width < 0 || height < 0 {
		return 0, false
	}
	limit := min(uint64(math.MaxUint32), uint64(math.MaxInt))
	w, h := uint64(width), uint64(height)
	if w > limit || h > limit || w != 0 && w*h > limit {
		return 0, false
	}
	return int(w * h), true
}

func (f Frame) validateStorage() error {
	if f.page == nil {
		return fmt.Errorf("missing frame page")
	}
	cellCount, ok := frameCellCount(f.Width, f.Height)
	if !ok {
		return fmt.Errorf("frame cell count exceeds page limit")
	}
	if len(f.page.cells) != cellCount {
		return fmt.Errorf("invalid cell count: got %d want %d", len(f.page.cells), cellCount)
	}
	if len(f.page.rows) != f.Height {
		return fmt.Errorf("row descriptor count: got %d want %d", len(f.page.rows), f.Height)
	}
	return nil
}

func (f Frame) Validate() error {
	if f.Width <= 0 || f.Height <= 0 {
		return fmt.Errorf("invalid frame size %dx%d", f.Width, f.Height)
	}
	return f.validateStorage()
}

// CheckInvariants validates row ownership, style references, dictionary keys,
// and exact reference counts. It is intended for tests and assertions.
func (f Frame) CheckInvariants() error {
	if err := f.validateStorage(); err != nil {
		return err
	}
	seenRows := make([]bool, f.Height)
	refs := make([]uint32, len(f.page.styles))
	for y, offset := range f.page.rows {
		if f.Width == 0 {
			if offset != 0 {
				return fmt.Errorf("row %d: nonzero offset %d for zero width", y, offset)
			}
			continue
		}
		if offset%uint32(f.Width) != 0 {
			return fmt.Errorf("row %d: offset %d is not a multiple of width %d", y, offset, f.Width)
		}
		physical := int(offset) / f.Width
		if physical >= f.Height || seenRows[physical] {
			return fmt.Errorf("row %d: invalid or duplicate physical row %d", y, physical)
		}
		seenRows[physical] = true
	}
	for i, cell := range f.page.cells {
		if int(cell.styleID) >= len(f.page.styles) || !f.page.styles[cell.styleID].used {
			return fmt.Errorf("cell %d: unresolved style ID %d", i, cell.styleID)
		}
		refs[cell.styleID]++
	}
	free := make([]bool, len(f.page.styles))
	for position, id := range f.page.freeStyles {
		if int(id) >= len(f.page.styles) {
			return fmt.Errorf("free style entry %d has out-of-range ID %d", position, id)
		}
		if f.page.styles[id].used {
			return fmt.Errorf("free style entry %d identifies used style %d", position, id)
		}
		if free[id] {
			return fmt.Errorf("free style ID %d appears more than once", id)
		}
		free[id] = true
	}
	var used uint32
	for id, slot := range f.page.styles {
		if !slot.used {
			if slot.refs != 0 {
				return fmt.Errorf("free style %d has %d references", id, slot.refs)
			}
			if !free[id] {
				return fmt.Errorf("unused style %d is missing from free list", id)
			}
			continue
		}
		used++
		if slot.refs != refs[id] {
			return fmt.Errorf("style %d references = %d want %d", id, slot.refs, refs[id])
		}
		if slot.style != slot.style.Canonical() {
			return fmt.Errorf("style %d is not canonical", id)
		}
		if indexed, ok := f.page.styleIndex[slot.style]; !ok || indexed != uint32(id) {
			return fmt.Errorf("style %d missing from index", id)
		}
	}
	if len(f.page.styles) == 0 || !f.page.styles[0].used || f.page.styles[0].style != DefaultStyle() {
		return fmt.Errorf("style ID zero is not the default style")
	}
	if used != f.page.styleCount || len(f.page.styleIndex) != int(used) {
		return fmt.Errorf("style count = %d/%d, index entries %d", used, f.page.styleCount, len(f.page.styleIndex))
	}
	return f.checkPayloadInvariants()
}

// LogicalBytes returns deterministic uncompressed page bytes independent of Go
// pointer size, map capacity, and allocator overhead.
func (f Frame) LogicalBytes() uint64 {
	if f.page == nil {
		return 0
	}
	return uint64(len(f.page.cells))*StoredCellLogicalBytes + uint64(len(f.page.rows))*RowDescriptorLogicalBytes + uint64(f.page.styleCount)*StyleRecordLogicalBytes + f.page.payloadBytes
}

// StyleCount returns the number of live page-local styles, including default ID zero.
func (f Frame) StyleCount() int {
	if f.page == nil {
		return 0
	}
	return int(f.page.styleCount)
}

func (f Frame) Columns() int { return f.Width }
func (f Frame) Rows() int    { return f.Height }
func (f Frame) Cell(x, y int) Cell {
	stored := f.page.cells[f.offset(x, y)]
	return Cell{Rune: rune(stored.rune), Style: f.page.styles[stored.styleID].style, Payload: f.payload(stored.payloadID), Continuation: stored.flags&continuationFlag != 0}
}
func (f Frame) At(x, y int) Cell { return f.Cell(x, y) }

// IsContinuation reports whether (x,y) is the right half of a wide pair
// without materializing the cell.
func (f Frame) IsContinuation(x, y int) bool {
	return f.page.cells[f.offset(x, y)].flags&continuationFlag != 0
}

func (f Frame) Set(x, y int, cell Cell) {
	index := f.offset(x, y)
	payloadID := f.internPayload(cell.Payload)
	f.releasePayload(f.page.cells[index].payloadID)
	oldID := f.page.cells[index].styleID
	styleID := oldID
	// Repainting a cell usually keeps its style. Stored styles are canonical,
	// so a raw match proves the canonical form matches too; this skips
	// canonicalization and the dictionary for the common case.
	if f.page.styles[oldID].style != cell.Style {
		styleID = f.internStyle(cell.Style)
		f.releaseStyle(oldID)
	}
	flags := uint8(0)
	if cell.Continuation {
		flags = continuationFlag
	}
	f.page.cells[index] = storedCell{rune: int32(cell.Rune), styleID: styleID, payloadID: payloadID, flags: flags}
}

func (f Frame) Row(y int) []Cell {
	if y < 0 || y >= f.Height {
		panic("frame row out of range")
	}
	row := make([]Cell, f.Width)
	for x := range row {
		row[x] = f.Cell(x, y)
	}
	return row
}

func (f Frame) WriteRow(y, x int, cells []Cell) int {
	count := min(len(cells), f.Width-x)
	for i := range count {
		f.Set(x+i, y, cells[i])
	}
	return count
}

func (f Frame) CopyRow(y, dst, src, count int) {
	if count <= 0 || dst == src {
		return
	}
	if dst < src {
		for i := range count {
			f.Set(dst+i, y, f.Cell(src+i, y))
		}
		return
	}
	for i := count - 1; i >= 0; i-- {
		f.Set(dst+i, y, f.Cell(src+i, y))
	}
}

func (f Frame) FillRow(y, start, end int, cell Cell) {
	for x := max(0, start); x < min(f.Width, end); x++ {
		f.Set(x, y, cell)
	}
}

func (f Frame) ScrollUp(top, bottom, n int) {
	for ; n > 0; n-- {
		recycled := f.page.rows[top]
		copy(f.page.rows[top:bottom], f.page.rows[top+1:bottom+1])
		f.page.rows[bottom] = recycled
		f.blankPhysicalRow(recycled)
	}
}

func (f Frame) ScrollDown(top, bottom, n int) {
	for ; n > 0; n-- {
		recycled := f.page.rows[bottom]
		copy(f.page.rows[top+1:bottom+1], f.page.rows[top:bottom])
		f.page.rows[top] = recycled
		f.blankPhysicalRow(recycled)
	}
}

func (f Frame) offset(x, y int) int {
	if x < 0 || x >= f.Width {
		panic("frame column out of range")
	}
	if y < 0 || y >= f.Height {
		panic("frame row out of range")
	}
	return int(f.page.rows[y]) + x
}

func (f Frame) blankPhysicalRow(offset uint32) {
	row := f.page.cells[offset : int(offset)+f.Width]
	for i := range row {
		cell := &row[i]
		if cell.payloadID != 0 {
			f.releasePayload(cell.payloadID)
		}
		if oldID := cell.styleID; oldID != 0 {
			f.page.styles[0].refs++
			f.releaseStyle(oldID)
		}
	}
	if len(row) == 0 {
		return
	}
	row[0] = storedCell{rune: ' '}
	for filled := 1; filled < len(row); filled *= 2 {
		copy(row[filled:], row[:filled])
	}
}

// internStyle returns the page-local ID for style and acquires one reference.
func (f Frame) internStyle(raw Style) uint32 {
	p := f.page
	if p.styleCacheOK && raw == p.styleCache {
		p.styles[p.styleCacheID].refs++
		return p.styleCacheID
	}
	style := raw.Canonical()
	id, ok := p.styleIndex[style]
	if ok {
		p.styles[id].refs++
	} else {
		if n := len(p.freeStyles); n != 0 {
			id = p.freeStyles[n-1]
			p.freeStyles = p.freeStyles[:n-1]
			p.styles[id] = styleSlot{style: style, refs: 1, used: true}
		} else {
			id = uint32(len(p.styles))
			if len(p.styles) == cap(p.styles) {
				p.styles = f.growStyles()
			}
			p.styles = append(p.styles, styleSlot{style: style, refs: 1, used: true})
		}
		p.styleIndex[style] = id
		p.styleCount++
	}
	p.styleCache, p.styleCacheID, p.styleCacheOK = raw, id, true
	return id
}

// growStyles doubles style capacity instead of Go's gentler large-slice growth,
// roughly halving total bytes allocated by high-cardinality frames. Capacity
// never exceeds the one-style-per-cell bound, so small frames stay small.
func (f Frame) growStyles() []styleSlot {
	old := f.page.styles
	limit := len(f.page.cells) + 1
	newCap := min(max(2*cap(old), 4), max(limit, len(old)+1))
	grown := make([]styleSlot, len(old), newCap)
	copy(grown, old)
	return grown
}

func (f Frame) releaseStyle(id uint32) {
	slot := &f.page.styles[id]
	slot.refs--
	if id == 0 || slot.refs != 0 {
		return
	}
	if f.page.styleCacheID == id {
		f.page.styleCacheOK = false
	}
	delete(f.page.styleIndex, slot.style)
	*slot = styleSlot{}
	f.page.freeStyles = append(f.page.freeStyles, id)
	f.page.styleCount--
}
