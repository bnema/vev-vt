package vt

import (
	"errors"
	"fmt"
	"math"
	"slices"

	renderer "github.com/bnema/vev-vt/core"
)

// ErrHistoryRowTooLarge is returned when one row cannot fit within the
// configured logical-byte budget or compact page bounds. History is unchanged.
var ErrHistoryRowTooLarge = errors.New("history row exceeds logical byte capacity")

var errInvalidHistoryRowID = errors.New("invalid history row ID")

const maxTailPreallocCells = 32 * 1024

// HistoryConfig controls the bounded terminal history retained by a Screen.
type HistoryConfig struct {
	MaxRows   int
	MaxBytes  uint64
	ChunkRows int
}

// History stores terminal rows in immutable chunks. It is intended to be
// mutated by the owner of a Screen; views are safe to retain after later
// appends.
type History struct {
	maxRows           int
	maxBytes          uint64
	chunkRows         int
	chunks            []*HistoryChunk
	tail              [][]renderer.Cell
	tailCells         []renderer.Cell
	tailBounds        []LineBound
	tailIDs           []RowID
	tailBytes         uint64
	tailPageWidth     int
	tailPageRows      int
	tailPageStyles    map[renderer.Style]struct{}
	styleScratch      map[renderer.Style]struct{}
	payloadScratch    map[renderer.CellPayload]struct{}
	tailPagePayloads  map[renderer.CellPayload]struct{}
	rows              int
	cells             int
	logicalBytes      uint64
	nextRowID         RowID
	compressionCursor int
}

// HistoryChunk is an immutable compact slab of equal-width history rows. Its
// identity is stable and can be used by consumers to reuse unchanged chunks.
type HistoryChunk struct {
	page         *sealedPage
	start        int
	count        int
	width        int
	bounds       []LineBound
	rowIDs       []RowID
	styleDrops   []uint64
	styleCount   uint64
	payloadDrops []uint64
	payloadBytes uint64
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
		lastStyleRow := make(map[renderer.Style]int)
		for i := start; i < end; i++ {
			frame.WriteRow(i-start, 0, rows[i])
			for _, cell := range rows[i] {
				style := cell.Style.Canonical()
				if style != renderer.DefaultStyle() {
					lastStyleRow[style] = i - start
				}
			}
		}
		styleDrops := make([]uint64, end-start)
		for _, row := range lastStyleRow {
			styleDrops[row]++
		}
		chunks = append(chunks, &HistoryChunk{
			page:       newSealedPage(frame),
			count:      end - start,
			width:      width,
			bounds:     append([]LineBound(nil), bounds[start:end]...),
			rowIDs:     append([]RowID(nil), rowIDs[start:end]...),
			styleDrops: styleDrops,
			styleCount: uint64(len(lastStyleRow)) + 1,
		})
		chunks[len(chunks)-1].recordPayloads()
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
	return c.frameView().Row(c.start + i)
}

func (c *HistoryChunk) cell(row, column int) renderer.Cell {
	return c.frameView().Cell(column, c.start+row)
}

func (c *HistoryChunk) cells() int { return c.count * c.width }

// CheckInvariants validates one compact immutable history slab.
func (c *HistoryChunk) CheckInvariants() error {
	if c == nil || c.page == nil {
		return fmt.Errorf("nil history chunk backing")
	}
	frame, err := c.page.readFrame(true)
	if err != nil {
		return err
	}
	if c.start < 0 || c.count <= 0 || c.width < 0 || c.start > frame.Height-c.count {
		return fmt.Errorf("invalid history chunk range start=%d count=%d", c.start, c.count)
	}
	if frame.Width != c.width || len(c.bounds) != c.count || len(c.rowIDs) != c.count || len(c.styleDrops) != frame.Height {
		return fmt.Errorf("history chunk metadata is not parallel")
	}
	styles := uint64(1)
	for _, dropped := range c.styleDrops[c.start:] {
		styles += dropped
	}
	if c.styleCount != styles {
		return fmt.Errorf("history chunk style count = %d want %d", c.styleCount, styles)
	}
	var payloadBytes uint64
	if len(c.payloadDrops) != 0 {
		if len(c.payloadDrops) != frame.Height {
			return fmt.Errorf("history payload metadata is not parallel")
		}
		for _, dropped := range c.payloadDrops[c.start:] {
			payloadBytes += dropped
		}
	}
	if payloadBytes != c.payloadBytes {
		return fmt.Errorf("history payload accounting mismatch")
	}
	if err := frame.CheckInvariants(); err != nil {
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
	chunks       []*HistoryChunk
	rows         int
	cells        int
	logicalBytes uint64
	nextRowID    RowID
}

// HistorySnapshotView captures sealed history chunks and the mutable tail
// independently. Sealed chunks are shared by identity; Tail is owned by the
// view and can be serialized without rotating the live tail into a chunk.
type HistorySnapshotView struct {
	chunks       []*HistoryChunk
	tail         HistoryView
	rows         int
	cells        int
	logicalBytes uint64
	nextRowID    RowID
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
	maxBytes := config.MaxBytes
	if maxBytes == 0 {
		maxBytes = defaultHistoryMaxBytes(config.MaxRows, chunkRows)
	}
	return &History{maxRows: config.MaxRows, maxBytes: maxBytes, chunkRows: chunkRows, nextRowID: 1}
}

func defaultHistoryMaxBytes(maxRows, chunkRows int) uint64 {
	const defaultRowBytes = 160*renderer.StoredCellLogicalBytes + renderer.RowDescriptorLogicalBytes
	rows := uint64(maxRows)
	pages := (rows + uint64(chunkRows) - 1) / uint64(chunkRows)
	if rows > math.MaxUint64/defaultRowBytes {
		return math.MaxUint64
	}
	bytes := rows * defaultRowBytes
	if pages > (math.MaxUint64-bytes)/renderer.StyleRecordLogicalBytes {
		return math.MaxUint64
	}
	return bytes + pages*renderer.StyleRecordLogicalBytes
}

func historyRowBaseLogicalBytes(width int) uint64 {
	return uint64(width)*renderer.StoredCellLogicalBytes + renderer.RowDescriptorLogicalBytes
}

func addLogicalBytes(total, value uint64) uint64 {
	if value > math.MaxUint64-total {
		return math.MaxUint64
	}
	return total + value
}

func historyChunkLogicalBytes(chunk *HistoryChunk) uint64 {
	if chunk == nil {
		return 0
	}
	return uint64(chunk.len())*historyRowBaseLogicalBytes(chunk.width) + chunk.styleCount*renderer.StyleRecordLogicalBytes + chunk.payloadBytes
}

func historyChunksLogicalBytes(chunks []*HistoryChunk) uint64 {
	var total uint64
	for _, chunk := range chunks {
		total = addLogicalBytes(total, historyChunkLogicalBytes(chunk))
	}
	return total
}

func (h *History) prepareRowStyles(row []renderer.Cell) uint64 {
	clear(h.payloadScratch)
	if h.styleScratch == nil {
		h.styleScratch = make(map[renderer.Style]struct{})
	} else {
		clear(h.styleScratch)
	}
	if len(row) > 0 {
		firstRaw := row[0].Style
		same := true
		for _, cell := range row {
			h.recordPayloadScratch(cell.Payload)
			if cell.Style != firstRaw {
				same = false
			}
		}
		if same {
			first := firstRaw.Canonical()
			if first != renderer.DefaultStyle() {
				h.styleScratch[first] = struct{}{}
			}
		} else {
			for _, cell := range row {
				style := cell.Style.Canonical()
				if style != renderer.DefaultStyle() {
					h.styleScratch[style] = struct{}{}
				}
			}
		}
	}
	return historyRowBaseLogicalBytes(len(row)) + renderer.StyleRecordLogicalBytes + uint64(len(h.styleScratch))*renderer.StyleRecordLogicalBytes + h.rowPayloadBytes()
}

func (h *History) prepareChunkRowStyles(chunk *HistoryChunk, row int) uint64 {
	clear(h.payloadScratch)
	if h.styleScratch == nil {
		h.styleScratch = make(map[renderer.Style]struct{})
	} else {
		clear(h.styleScratch)
	}
	for column := range chunk.width {
		cell := chunk.cell(row, column)
		h.recordPayloadScratch(cell.Payload)
		style := cell.Style.Canonical()
		if style != renderer.DefaultStyle() {
			h.styleScratch[style] = struct{}{}
		}
	}
	return historyRowBaseLogicalBytes(chunk.width) + renderer.StyleRecordLogicalBytes + uint64(len(h.styleScratch))*renderer.StyleRecordLogicalBytes + h.rowPayloadBytes()
}

func (h *History) tailAppendDelta(width int) uint64 {
	newPage := h.tailPageRows == 0 || h.tailPageWidth != width || h.tailPageRows >= maxHistorySlabRows(width)
	delta := historyRowBaseLogicalBytes(width)
	if newPage {
		return delta + renderer.StyleRecordLogicalBytes + uint64(len(h.styleScratch))*renderer.StyleRecordLogicalBytes + h.rowPayloadBytes()
	}
	for style := range h.styleScratch {
		if _, ok := h.tailPageStyles[style]; !ok {
			delta += renderer.StyleRecordLogicalBytes
		}
	}
	for p := range h.payloadScratch {
		if _, ok := h.tailPagePayloads[p]; !ok {
			delta += p.LogicalBytes()
		}
	}
	return delta
}

func (h *History) recordTailRowStyles(width int) {
	if h.tailPageRows == 0 || h.tailPageWidth != width || h.tailPageRows >= maxHistorySlabRows(width) {
		h.tailPageWidth = width
		h.tailPageRows = 0
		h.tailPageStyles = map[renderer.Style]struct{}{renderer.DefaultStyle(): {}}
		h.tailPagePayloads = nil
	}
	for p := range h.payloadScratch {
		if h.tailPagePayloads == nil {
			h.tailPagePayloads = make(map[renderer.CellPayload]struct{})
		}
		h.tailPagePayloads[p] = struct{}{}
	}
	for style := range h.styleScratch {
		h.tailPageStyles[style] = struct{}{}
	}
	h.tailPageRows++
}

func (h *History) rebuildTailAccounting() {
	old := h.tailBytes
	h.tailBytes = 0
	h.tailPageWidth = 0
	h.tailPageRows = 0
	h.tailPageStyles = nil
	h.tailPagePayloads = nil
	for _, row := range h.tail {
		h.prepareRowStyles(row)
		delta := h.tailAppendDelta(len(row))
		h.tailBytes += delta
		h.recordTailRowStyles(len(row))
	}
	h.logicalBytes = h.logicalBytes - old + h.tailBytes
}

// Append records a copy of row along with its logical extent and an
// automatically allocated nonzero ID. Once a chunk is full it is sealed
// forever. Rows larger than the logical-byte capacity are rejected without
// mutation.
func (h *History) Append(row []renderer.Cell, bound LineBound) error {
	if h == nil || h.maxRows == 0 {
		return nil
	}
	defer func() { clear(h.payloadScratch) }()
	if uint64(len(row)) > uint64(math.MaxUint32) || historyRowBaseLogicalBytes(len(row))+renderer.StyleRecordLogicalBytes > h.maxBytes || h.prepareRowStyles(row) > h.maxBytes {
		return ErrHistoryRowTooLarge
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
	defer func() { clear(h.payloadScratch) }()
	if uint64(len(row)) > uint64(math.MaxUint32) || historyRowBaseLogicalBytes(len(row))+renderer.StyleRecordLogicalBytes > h.maxBytes || h.prepareRowStyles(row) > h.maxBytes {
		return ErrHistoryRowTooLarge
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
	if id >= ^RowID(0)-1 {
		return errInvalidHistoryRowID
	}
	if id >= h.nextRowID {
		h.nextRowID = id + 1
	}
	// Make room before adding so accounting cannot overflow even when the
	// configured capacities use their maximum values.
	h.evictFor(row)
	delta := h.tailAppendDelta(len(row))
	h.appendTailRow(row)
	h.recordTailRowStyles(len(row))
	h.tailBytes += delta
	h.logicalBytes += delta
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

func (h *History) reserveTailRow(width int) (start, end int) {
	start = len(h.tailCells)
	end = start + width
	if end > cap(h.tailCells) {
		capacity := end
		if cap(h.tailCells) <= (math.MaxInt-1)/2 {
			capacity = max(capacity, cap(h.tailCells)*2)
		}
		if len(h.tailCells) == 0 {
			prealloc := uint64(maxTailPreallocCells)
			prealloc = min(prealloc, h.maxBytes/renderer.StoredCellLogicalBytes)
			capacity = max(capacity, max(width, int(prealloc)))
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
	chunks := newHistoryChunks(h.tail, h.tailBounds, h.tailIDs)
	h.chunks = append(h.chunks, chunks...)
	h.logicalBytes = h.logicalBytes - h.tailBytes + historyChunksLogicalBytes(chunks)
	h.tail = nil
	h.tailCells = nil
	h.tailBounds = nil
	h.tailIDs = nil
	h.tailBytes = 0
	h.tailPageWidth = 0
	h.tailPageRows = 0
	h.tailPageStyles = nil
	h.tailPagePayloads = nil
}

// evictFor discards oldest rows until row can fit both retention budgets.
func (h *History) evictFor(row []renderer.Cell) {
	for {
		delta := h.tailAppendDelta(len(row))
		if h.rows <= h.maxRows-1 && h.logicalBytes <= h.maxBytes-delta {
			return
		}
		h.evictOldest()
		h.prepareRowStyles(row)
	}
}

func (h *History) evictOldest() {
	if h.rows == 0 {
		return
	}
	if len(h.chunks) > 0 {
		chunk := h.chunks[0]
		oldBytes := historyChunkLogicalBytes(chunk)
		h.rows--
		h.cells -= chunk.width
		if chunk.count == 1 {
			h.logicalBytes -= oldBytes
			copy(h.chunks, h.chunks[1:])
			h.chunks[len(h.chunks)-1] = nil
			h.chunks = h.chunks[:len(h.chunks)-1]
		} else {
			// Preserve cell storage while replacing only the chunk wrapper: a
			// retained view may still refer to the original immutable chunk. The
			// wrapper tracks only styles still used by its retained suffix.
			h.chunks[0] = chunk.withoutFirstRow()
			h.logicalBytes -= oldBytes - historyChunkLogicalBytes(h.chunks[0])
		}
		return
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
	h.rebuildTailAccounting()
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
	return HistoryView{chunks: chunks, rows: h.rows, cells: h.cells, logicalBytes: h.logicalBytes, nextRowID: h.nextRowID}
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
	tailChunks := newHistoryChunks(h.tail, h.tailBounds, h.tailIDs)
	return HistorySnapshotView{
		chunks: append([]*HistoryChunk(nil), h.chunks...),
		tail: HistoryView{
			chunks:       tailChunks,
			rows:         len(h.tail),
			cells:        historyRowsCells(h.tail),
			logicalBytes: historyChunksLogicalBytes(tailChunks),
			nextRowID:    h.nextRowID,
		},
		rows:         h.rows,
		cells:        h.cells,
		logicalBytes: h.logicalBytes,
		nextRowID:    h.nextRowID,
	}
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

// LogicalBytes returns deterministic uncompressed bytes retained by history.
// It excludes the live primary and alternate screens and has no page allowance.
func (h *History) LogicalBytes() uint64 {
	if h == nil {
		return 0
	}
	return h.logicalBytes
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

// ByteCap returns the configured uncompressed logical-byte capacity.
func (h *History) ByteCap() uint64 {
	if h == nil {
		return 0
	}
	return h.maxBytes
}

func (v HistoryView) Len() int   { return v.rows }
func (v HistoryView) Cells() int { return v.cells }
func (v HistoryView) LogicalBytes() uint64 {
	if v.logicalBytes != 0 || len(v.chunks) == 0 {
		return v.logicalBytes
	}
	return historyChunksLogicalBytes(v.chunks)
}
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
		// Streaming search restores at most one transient page at a time and
		// does not populate the random-access cache of every cold page.
		frame, err := chunk.page.readFrame(false)
		if err != nil {
			panic(err)
		}
		for i := range chunk.len() {
			if !yield(frame.Row(chunk.start + i)) {
				return
			}
		}
	}
}

// Row returns a decoded owned copy of the row at i, or nil when out of range.
func (v HistoryView) Row(i int) []renderer.Cell {
	chunk, row := v.locateRow(i)
	if chunk == nil {
		return nil
	}
	return chunk.row(row)
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

func (v HistorySnapshotView) Len() int   { return v.rows }
func (v HistorySnapshotView) Cells() int { return v.cells }
func (v HistorySnapshotView) LogicalBytes() uint64 {
	if v.logicalBytes != 0 || v.rows == 0 {
		return v.logicalBytes
	}
	return addLogicalBytes(historyChunksLogicalBytes(v.chunks), v.tail.LogicalBytes())
}
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
	tail := v.tail
	tail.nextRowID = v.nextRowID
	return tail
}

func historyRowsCells(rows [][]renderer.Cell) int {
	cells := 0
	for _, row := range rows {
		cells += len(row)
	}
	return cells
}
