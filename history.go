package vt

import (
	"errors"
	"fmt"
	"math"
	"slices"

	renderer "github.com/bnema/vev-vt/core"
)

// ErrHistoryRowTooWide is returned when a row cannot fit within the configured
// cell budget. The history is not modified when this error is returned.
var ErrHistoryRowTooWide = errors.New("history row exceeds cell capacity")

var errInvalidHistoryRowID = errors.New("invalid history row ID")

const maxTailPreallocCells = 32 * 1024

// HistoryConfig controls the bounded terminal history retained by a Screen.
type HistoryConfig struct {
	MaxRows   int
	MaxCells  int
	ChunkRows int
}

// History stores terminal rows in immutable chunks. It is intended to be
// mutated by the owner of a Screen; views are safe to retain after later
// appends.
type History struct {
	maxRows    int
	maxCells   int
	chunkRows  int
	chunks     []*HistoryChunk
	tail       [][]renderer.Cell
	tailCells  []renderer.Cell
	tailBounds []LineBound
	tailIDs    []RowID
	rows       int
	cells      int
	nextRowID  RowID
}

// HistoryChunk is an immutable compact slab of equal-width history rows. Its
// identity is stable and can be used by consumers to reuse unchanged chunks.
type HistoryChunk struct {
	frame  renderer.Frame
	start  int
	count  int
	width  int
	bounds []LineBound
	rowIDs []RowID
}

func newHistoryChunks(rows [][]renderer.Cell, bounds []LineBound, rowIDs []RowID) []*HistoryChunk {
	bounds = growBounds(bounds, len(rows))
	rowIDs = growRowIDs(rowIDs, len(rows))
	var chunks []*HistoryChunk
	for start := 0; start < len(rows); {
		width := len(rows[start])
		limit := start + min(len(rows)-start, maxHistorySlabRows(width))
		end := start + 1
		for end < limit && len(rows[end]) == width {
			end++
		}
		frame := renderer.NewFrame(width, end-start)
		for i := start; i < end; i++ {
			frame.WriteRow(i-start, 0, rows[i])
		}
		chunks = append(chunks, &HistoryChunk{
			frame:  frame,
			count:  end - start,
			width:  width,
			bounds: append([]LineBound(nil), bounds[start:end]...),
			rowIDs: append([]RowID(nil), rowIDs[start:end]...),
		})
		start = end
	}
	return chunks
}

func maxHistorySlabRows(width int) int {
	if width <= 0 {
		return math.MaxInt
	}
	rows := min(uint64(math.MaxUint32)/uint64(width), uint64(math.MaxInt))
	return max(int(rows), 1)
}

func (c *HistoryChunk) len() int {
	if c == nil {
		return 0
	}
	return c.count
}

func (c *HistoryChunk) row(i int) []renderer.Cell {
	if c == nil || i < 0 || i >= c.count {
		return nil
	}
	return c.frame.Row(c.start + i)
}

func (c *HistoryChunk) cell(row, column int) renderer.Cell {
	return c.frame.Cell(column, c.start+row)
}

func (c *HistoryChunk) cells() int { return c.count * c.width }

// CheckInvariants validates one compact immutable history slab.
func (c *HistoryChunk) CheckInvariants() error {
	if c == nil {
		return fmt.Errorf("nil history chunk")
	}
	if c.start < 0 || c.count <= 0 || c.width < 0 || c.start > c.frame.Height-c.count {
		return fmt.Errorf("invalid history chunk range start=%d count=%d", c.start, c.count)
	}
	if c.frame.Width != c.width || len(c.bounds) != c.count || len(c.rowIDs) != c.count {
		return fmt.Errorf("history chunk metadata is not parallel")
	}
	if err := c.frame.CheckInvariants(); err != nil {
		return fmt.Errorf("history chunk frame: %w", err)
	}
	for i, id := range c.rowIDs {
		if id == 0 || id >= ^RowID(0)-1 {
			return fmt.Errorf("history row %d has invalid ID %d", i, id)
		}
		if !validHistoryBound(c.bounds[i], c.width) {
			return fmt.Errorf("history row %d has invalid bound", i)
		}
	}
	return nil
}

// HistoryView is an immutable snapshot of history. Row returns a copy so the
// storage behind a sealed chunk remains owned by VT.
type HistoryView struct {
	chunks    []*HistoryChunk
	rows      int
	cells     int
	nextRowID RowID
}

// HistorySnapshotView captures sealed history chunks and the mutable tail
// independently. Sealed chunks are shared by identity; Tail is owned by the
// view and can be serialized without rotating the live tail into a chunk.
type HistorySnapshotView struct {
	chunks     []*HistoryChunk
	tail       [][]renderer.Cell
	tailBounds []LineBound
	tailIDs    []RowID
	rows       int
	cells      int
	nextRowID  RowID
}

func NewHistory(config HistoryConfig) *History {
	if config.MaxRows <= 0 {
		return &History{}
	}
	chunkRows := config.ChunkRows
	if chunkRows <= 0 {
		chunkRows = 256
	}
	chunkRows = min(chunkRows, 256)
	chunkRows = min(chunkRows, config.MaxRows)
	maxCells := config.MaxCells
	if maxCells <= 0 {
		maxCells = defaultHistoryMaxCells(config.MaxRows)
	}
	return &History{maxRows: config.MaxRows, maxCells: maxCells, chunkRows: chunkRows, nextRowID: 1}
}

func defaultHistoryMaxCells(maxRows int) int {
	if maxRows > math.MaxInt/160 {
		return math.MaxInt
	}
	return maxRows * 160
}

// Append records a copy of row along with its logical extent and an
// automatically allocated nonzero ID. Once a chunk is full it is sealed
// forever. Rows wider than the total cell capacity are rejected without
// mutation.
func (h *History) Append(row []renderer.Cell, bound LineBound) error {
	if h == nil || h.maxRows == 0 {
		return nil
	}
	if uint64(len(row)) > uint64(math.MaxUint32) || len(row) > h.maxCells {
		return ErrHistoryRowTooWide
	}
	id, err := h.allocateRowID()
	if err != nil {
		return err
	}
	return h.appendRow(row, bound, id)
}

// AppendWithID records a copy of row with an explicit persisted identity.
// Unlike Append, malformed identities are rejected rather than synthesized.
func (h *History) AppendWithID(row []renderer.Cell, bound LineBound, id RowID) error {
	if id == 0 || id >= ^RowID(0)-1 {
		return errInvalidHistoryRowID
	}
	if h == nil || h.maxRows == 0 {
		return nil
	}
	if uint64(len(row)) > uint64(math.MaxUint32) || len(row) > h.maxCells {
		return ErrHistoryRowTooWide
	}
	if h.hasRowID(id) {
		return errInvalidHistoryRowID
	}
	return h.appendRow(row, bound, id)
}

func (h *History) appendRow(row []renderer.Cell, bound LineBound, id RowID) error {
	if h.nextRowID == ^RowID(0) {
		return errInvalidHistoryRowID
	}
	if uint64(len(row)) > uint64(math.MaxUint32) || len(row) > h.maxCells {
		return ErrHistoryRowTooWide
	}
	if id >= ^RowID(0)-1 {
		return errInvalidHistoryRowID
	}
	if id >= h.nextRowID {
		h.nextRowID = id + 1
	}
	// Make room before adding so Cells cannot overflow even when the default
	// capacity is MaxInt.
	h.evictFor(len(row))
	h.appendTailRow(row)
	h.tailBounds = append(h.tailBounds, clampBound(bound, len(row)))
	h.tailIDs = append(h.tailIDs, id)
	h.rows++
	h.cells += len(row)
	if len(h.tail) == h.chunkRows {
		h.sealTail()
	}
	return nil
}

// allocateRowID relies on appendRow advancing nextRowID above every accepted
// explicit ID; automatic IDs therefore never need to scan retained rows.
func (h *History) allocateRowID() (RowID, error) {
	if h.nextRowID == 0 {
		h.nextRowID = 1
	}
	if h.nextRowID >= ^RowID(0)-1 {
		return 0, errInvalidHistoryRowID
	}
	id := h.nextRowID
	h.nextRowID++
	return id, nil
}

func (h *History) hasRowID(id RowID) bool {
	for _, chunk := range h.chunks {
		if slices.Contains(chunk.rowIDs, id) {
			return true
		}
	}
	return slices.Contains(h.tailIDs, id)
}

// clampBound keeps End inside the row it describes so a persisted bound can be
// validated without consulting its row again.
func clampBound(b LineBound, cells int) LineBound {
	b.End = min(max(b.End, 0), cells)
	return b
}

// growBounds returns bounds sized to rows, padding with zero values. It keeps
// the parallel-slice invariant when a chunk arrives with fewer bounds than rows.
func growBounds(bounds []LineBound, rows int) []LineBound {
	if len(bounds) == rows {
		return bounds
	}
	out := make([]LineBound, rows)
	copy(out, bounds)
	return out
}

func growRowIDs(ids []RowID, rows int) []RowID {
	if len(ids) == rows {
		return ids
	}
	out := make([]RowID, rows)
	copy(out, ids)
	return out
}

func cloneRowIDs(ids []RowID, rows int) []RowID {
	out := make([]RowID, rows)
	copy(out, ids)
	return out
}

func (h *History) reserveTailRow(width int) (start, end int) {
	start = len(h.tailCells)
	end = start + width
	if end > cap(h.tailCells) {
		capacity := end
		if cap(h.tailCells) <= (math.MaxInt-1)/2 {
			capacity = max(capacity, cap(h.tailCells)*2)
		}
		if len(h.tailCells) == 0 {
			capacity = max(capacity, min(h.maxCells, max(width, maxTailPreallocCells)))
		}
		cells := make([]renderer.Cell, len(h.tailCells), capacity)
		copy(cells, h.tailCells)
		h.tailCells = cells
		offset := 0
		for i := range h.tail {
			rowWidth := len(h.tail[i])
			h.tail[i] = h.tailCells[offset : offset+rowWidth : offset+rowWidth]
			offset += rowWidth
		}
	}
	h.tailCells = h.tailCells[:end]
	return start, end
}

func (h *History) appendTailRow(row []renderer.Cell) {
	start, end := h.reserveTailRow(len(row))
	copy(h.tailCells[start:end], row)
	h.tail = append(h.tail, h.tailCells[start:end:end])
}

func (h *History) appendTailChunkRow(chunk *HistoryChunk, row int) {
	start, end := h.reserveTailRow(chunk.width)
	for x := range chunk.width {
		h.tailCells[start+x] = chunk.cell(row, x)
	}
	h.tail = append(h.tail, h.tailCells[start:end:end])
}

func (h *History) sealTail() {
	if len(h.tail) == 0 {
		return
	}
	h.chunks = append(h.chunks, newHistoryChunks(h.tail, h.tailBounds, h.tailIDs)...)
	h.tail = nil
	h.tailCells = nil
	h.tailBounds = nil
	h.tailIDs = nil
}

// normalizeTail seals complete chunks so the mutable tail remains shorter than
// chunkRows. Restored snapshots may have been written with a different chunk
// size than the current history configuration.
func (h *History) normalizeTail() {
	h.tailIDs = growRowIDs(h.tailIDs, len(h.tail))
	for h.chunkRows > 0 && len(h.tail) >= h.chunkRows {
		bounds := growBounds(h.tailBounds, len(h.tail))
		h.chunks = append(h.chunks, newHistoryChunks(h.tail[:h.chunkRows], bounds[:h.chunkRows], h.tailIDs[:h.chunkRows])...)
		removedCells := historyRowsCells(h.tail[:h.chunkRows])
		h.tail = h.tail[h.chunkRows:]
		h.tailCells = h.tailCells[removedCells:]
		h.tailBounds = bounds[h.chunkRows:]
		h.tailIDs = h.tailIDs[h.chunkRows:]
	}
}

func (h *History) evict() {
	h.evictUntil(0, 0)
}

// evictFor discards oldest rows until another row using cellCount cells can fit.
func (h *History) evictFor(cellCount int) {
	h.evictUntil(1, cellCount)
}

// evictUntil makes room for rowCount rows and cellCount cells without
// overflowing either accounting total.
func (h *History) evictUntil(rowCount, cellCount int) {
	for h.rows > h.maxRows-rowCount || h.cells > h.maxCells-cellCount {
		if len(h.chunks) > 0 {
			chunk := h.chunks[0]
			h.rows--
			h.cells -= chunk.width
			if chunk.count == 1 {
				copy(h.chunks, h.chunks[1:])
				h.chunks[len(h.chunks)-1] = nil
				h.chunks = h.chunks[:len(h.chunks)-1]
			} else {
				// Preserve cell storage while replacing only the chunk wrapper: a
				// retained view may still refer to the original immutable chunk.
				h.chunks[0] = &HistoryChunk{
					frame:  chunk.frame,
					start:  chunk.start + 1,
					count:  chunk.count - 1,
					width:  chunk.width,
					bounds: chunk.bounds[1:],
					rowIDs: chunk.rowIDs[1:],
				}
			}
			continue
		}
		if len(h.tail) == 0 {
			return
		}
		row := h.tail[0]
		h.rows--
		h.cells -= len(row)
		h.tail[0] = nil
		h.tail = h.tail[1:]
		h.tailCells = h.tailCells[len(row):]
		h.tailBounds = growBounds(h.tailBounds, len(h.tail)+1)[1:]
		h.tailIDs = growRowIDs(h.tailIDs, len(h.tail)+1)[1:]
	}
}

// SealAndView rotates the mutable tail into an immutable chunk, then captures
// the sealed chunks by identity. Callers must synchronize access to History.
func (h *History) SealAndView() HistoryView {
	if h != nil {
		h.sealTail()
	}
	return h.View()
}

// View captures the current history. Sealed chunks are shared by identity; a
// partially-filled tail is copied into a new immutable chunk for this view.
func (h *History) View() HistoryView {
	if h == nil {
		return HistoryView{}
	}
	if h.rows == 0 {
		return HistoryView{nextRowID: h.nextRowID}
	}
	chunks := make([]*HistoryChunk, len(h.chunks), len(h.chunks)+1)
	copy(chunks, h.chunks)
	if len(h.tail) > 0 {
		chunks = append(chunks, newHistoryChunks(h.tail, h.tailBounds, h.tailIDs)...)
	}
	return HistoryView{chunks: chunks, rows: h.rows, cells: h.cells, nextRowID: h.nextRowID}
}

// SnapshotView captures history for persistence without sealing the mutable
// tail. Sealed chunks are shared by identity and the tail is deeply copied.
func (h *History) SnapshotView() HistorySnapshotView {
	if h == nil {
		return HistorySnapshotView{}
	}
	if h.rows == 0 {
		return HistorySnapshotView{nextRowID: h.nextRowID}
	}
	return HistorySnapshotView{
		chunks:     append([]*HistoryChunk(nil), h.chunks...),
		tail:       cloneHistoryRows(h.tail),
		tailBounds: append([]LineBound(nil), growBounds(h.tailBounds, len(h.tail))...),
		tailIDs:    cloneRowIDs(h.tailIDs, len(h.tail)),
		rows:       h.rows,
		cells:      h.cells,
		nextRowID:  h.nextRowID,
	}
}

func cloneHistoryRows(rows [][]renderer.Cell) [][]renderer.Cell {
	if len(rows) == 0 {
		return nil
	}
	cells := make([]renderer.Cell, historyRowsCells(rows))
	copyRows := make([][]renderer.Cell, len(rows))
	offset := 0
	for i, row := range rows {
		end := offset + len(row)
		copy(cells[offset:end], row)
		copyRows[i] = cells[offset:end:end]
		offset = end
	}
	return copyRows
}

// Len returns the currently retained history row count.
func (h *History) Len() int {
	if h == nil {
		return 0
	}
	return h.rows
}

// Cells returns the currently retained history cell count.
func (h *History) Cells() int {
	if h == nil {
		return 0
	}
	return h.cells
}

// NextRowID returns the next identity allocated by this history.
func (h *History) NextRowID() RowID {
	if h == nil || h.nextRowID == 0 {
		return 1
	}
	return h.nextRowID
}

// Cap returns the configured bounded row capacity.
func (h *History) Cap() int {
	if h == nil {
		return 0
	}
	return h.maxRows
}

// CellCap returns the configured bounded cell capacity.
func (h *History) CellCap() int {
	if h == nil {
		return 0
	}
	return h.maxCells
}

func (v HistoryView) Len() int        { return v.rows }
func (v HistoryView) Cells() int      { return v.cells }
func (v HistoryView) ChunkCount() int { return len(v.chunks) }

// NextRowID returns the next identity recorded by this view.
func (v HistoryView) NextRowID() RowID {
	if v.nextRowID == 0 {
		next, ok := historyViewNextRowID(v)
		if !ok {
			return 1
		}
		return next
	}
	return v.nextRowID
}

// Chunk returns the immutable chunk at i, or nil when i is out of range.
func (v HistoryView) Chunk(i int) *HistoryChunk {
	if i < 0 || i >= len(v.chunks) {
		return nil
	}
	return v.chunks[i]
}

// Range calls yield with a decoded owned row in oldest-first order.
func (v HistoryView) Range(yield func([]renderer.Cell) bool) {
	for _, chunk := range v.chunks {
		for i := range chunk.len() {
			if !yield(chunk.row(i)) {
				return
			}
		}
	}
}

// Row returns a decoded owned copy of the row at i, or nil when out of range.
func (v HistoryView) Row(i int) []renderer.Cell {
	if i < 0 {
		return nil
	}
	for _, chunk := range v.chunks {
		if i < chunk.len() {
			return chunk.row(i)
		}
		i -= chunk.len()
	}
	return nil
}

// RowID returns the identity of the row at i, or zero when i is out of range.
func (v HistoryView) RowID(i int) RowID {
	if i < 0 {
		return 0
	}
	for _, chunk := range v.chunks {
		if i < chunk.len() {
			if i < len(chunk.rowIDs) {
				return chunk.rowIDs[i]
			}
			return 0
		}
		i -= chunk.len()
	}
	return 0
}

// FindRowID returns the oldest-first index of id, or -1 when id is absent.
func (v HistoryView) FindRowID(id RowID) int {
	if id == 0 {
		return -1
	}
	index := 0
	for _, chunk := range v.chunks {
		for row := range chunk.len() {
			if row < len(chunk.rowIDs) && chunk.rowIDs[row] == id {
				return index
			}
			index++
		}
	}
	return -1
}

// RowID returns the identity of the row within an immutable chunk.
func (c *HistoryChunk) RowID(i int) RowID {
	if c == nil || i < 0 || i >= len(c.rowIDs) {
		return 0
	}
	return c.rowIDs[i]
}

// Bound returns the logical extent of the row at i, or the zero value when i is
// out of range. A zero value describes a hard row whose End is not meaningful.
func (v HistoryView) Bound(i int) LineBound {
	if i < 0 {
		return LineBound{}
	}
	for _, chunk := range v.chunks {
		if i < chunk.len() {
			if i < len(chunk.bounds) {
				return chunk.bounds[i]
			}
			return LineBound{}
		}
		i -= chunk.len()
	}
	return LineBound{}
}

func (v HistorySnapshotView) Len() int        { return v.rows }
func (v HistorySnapshotView) Cells() int      { return v.cells }
func (v HistorySnapshotView) ChunkCount() int { return len(v.chunks) }

// NextRowID returns the next identity recorded by this snapshot.
func (v HistorySnapshotView) NextRowID() RowID {
	if v.nextRowID == 0 {
		return v.Tail().NextRowID()
	}
	return v.nextRowID
}

// Chunk returns a sealed immutable chunk at i, or nil when i is out of range.
func (v HistorySnapshotView) Chunk(i int) *HistoryChunk {
	if i < 0 || i >= len(v.chunks) {
		return nil
	}
	return v.chunks[i]
}

// Tail returns an immutable view of the copied mutable tail.
func (v HistorySnapshotView) Tail() HistoryView {
	if len(v.tail) == 0 {
		return HistoryView{nextRowID: v.nextRowID}
	}
	return HistoryView{
		chunks:    newHistoryChunks(v.tail, v.tailBounds, v.tailIDs),
		rows:      len(v.tail),
		cells:     historyRowsCells(v.tail),
		nextRowID: v.nextRowID,
	}
}

func historyRowsCells(rows [][]renderer.Cell) int {
	cells := 0
	for _, row := range rows {
		cells += len(row)
	}
	return cells
}
