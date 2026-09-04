package vt

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	renderer "github.com/bnema/vev-vt/core"
)

const (
	historyMagic            = "VTC1"
	historyHeaderBytes      = 16
	historyChunkHeaderBytes = 12
	historyStoredCellBytes  = 9
	historyStyleBytes       = 37
	historyRowBytes         = 13
	historyBoundBytes       = 5
	maxHistoryChunkRows     = 256

	// Aggregate resource ceilings allow wide rows without tying persistence to
	// a particular terminal geometry. They apply before compact allocation.
	maxHistoryChunks         = 12_000
	maxHistoryRows           = 12_000
	maxHistoryRowCells       = 160
	maxHistoryCells          = maxHistoryRows * maxHistoryRowCells
	maxHistoryStylesPerChunk = maxHistoryCells + 1
	maxHistoryDecodeStyles   = maxHistoryCells + maxHistoryChunks
	maxHistoryDecodedBytes   = maxHistoryCells*renderer.StoredCellLogicalBytes +
		maxHistoryRows*renderer.RowDescriptorLogicalBytes +
		maxHistoryDecodeStyles*renderer.StyleRecordLogicalBytes
)

var errInvalidHistory = errors.New("invalid history payload")

type historyDecodeStats struct {
	chunks, rows, cells, styles, bytes uint64
}

// MarshalHistory serializes semantic cells with canonical chunk-local style
// dictionaries. Internal style IDs and page memory never cross this boundary.
func MarshalHistory(view HistoryView) ([]byte, error) {
	if view.rows < 0 || view.rows > maxHistoryRows || view.rows != historyViewRowCount(view) || len(view.chunks) > maxHistoryChunks {
		return nil, fmt.Errorf("marshal history: %w", errInvalidHistory)
	}
	nextID, ok := historyViewNextRowID(view)
	if !ok {
		return nil, fmt.Errorf("marshal history: %w", errInvalidHistory)
	}
	type encodedChunk struct {
		chunk  *HistoryChunk
		styles []renderer.Style
		ids    map[renderer.Style]uint32
	}
	encoded := make([]encodedChunk, 0, len(view.chunks))
	size := uint64(historyHeaderBytes)
	stats := historyDecodeStats{chunks: uint64(len(view.chunks))}
	for _, chunk := range view.chunks {
		if chunk == nil || chunk.len() == 0 || chunk.len() > maxHistoryChunkRows ||
			chunk.len() != len(chunk.rowIDs) || chunk.len() != len(chunk.bounds) || chunk.width < 0 ||
			uint64(chunk.width) > math.MaxUint32 {
			return nil, fmt.Errorf("marshal history: %w", errInvalidHistory)
		}
		cells := uint64(chunk.width) * uint64(chunk.len())
		if !addHistoryDecodeBudget(&stats.rows, uint64(chunk.len()), maxHistoryRows) ||
			!addHistoryDecodeBudget(&stats.cells, cells, maxHistoryCells) {
			return nil, fmt.Errorf("marshal history: %w", errInvalidHistory)
		}
		styles := []renderer.Style{renderer.DefaultStyle()}
		ids := map[renderer.Style]uint32{renderer.DefaultStyle(): 0}
		for row := range chunk.len() {
			if !validHistoryBound(chunk.bounds[row], chunk.width) {
				return nil, fmt.Errorf("marshal history: %w", errInvalidHistory)
			}
			for x := range chunk.width {
				cell := chunk.cell(row, x)
				if !validHistoryCell(cell) {
					return nil, fmt.Errorf("marshal history: %w", errInvalidHistory)
				}
				style := cell.Style.Canonical()
				if _, exists := ids[style]; !exists {
					ids[style] = uint32(len(styles))
					styles = append(styles, style)
				}
			}
		}
		if !addHistoryDecodeBudget(&stats.styles, uint64(len(styles)), maxHistoryDecodeStyles) ||
			!addHistoryLogicalBytes(&stats, cells, uint64(chunk.len()), uint64(len(styles))) {
			return nil, fmt.Errorf("marshal history: %w", errInvalidHistory)
		}
		size += historyChunkHeaderBytes + uint64(len(styles)-1)*historyStyleBytes + uint64(chunk.len())*historyRowBytes + cells*historyStoredCellBytes
		encoded = append(encoded, encodedChunk{chunk: chunk, styles: styles, ids: ids})
	}
	// Aggregate budgets bound size well below MaxInt even on 32-bit hosts.
	out := make([]byte, 0, int(size))
	out = append(out, historyMagic...)
	out = binary.BigEndian.AppendUint32(out, uint32(len(encoded)))
	out = binary.BigEndian.AppendUint64(out, uint64(nextID))
	for _, entry := range encoded {
		chunk, styles, ids := entry.chunk, entry.styles, entry.ids
		out = binary.BigEndian.AppendUint32(out, uint32(chunk.width))
		out = binary.BigEndian.AppendUint32(out, uint32(chunk.len()))
		out = binary.BigEndian.AppendUint32(out, uint32(len(styles)))
		for _, style := range styles[1:] {
			out = appendHistoryStyle(out, style)
		}
		for row, id := range chunk.rowIDs {
			out = binary.BigEndian.AppendUint64(out, uint64(id))
			out = appendHistoryBound(out, chunk.bounds[row])
		}
		for row := range chunk.len() {
			for x := range chunk.width {
				cell := chunk.cell(row, x)
				out = binary.BigEndian.AppendUint32(out, uint32(cell.Rune))
				out = binary.BigEndian.AppendUint32(out, ids[cell.Style.Canonical()])
				flag := byte(0)
				if cell.Continuation {
					flag = 1
				}
				out = append(out, flag)
			}
		}
	}
	return out, nil
}

func historyViewRowCount(view HistoryView) int {
	n := 0
	for _, chunk := range view.chunks {
		if chunk == nil || chunk.len() > math.MaxInt-n {
			return -1
		}
		n += chunk.len()
	}
	return n
}

func historyViewNextRowID(view HistoryView) (RowID, bool) {
	seen := make(map[RowID]struct{}, view.rows)
	var maxID RowID
	for _, chunk := range view.chunks {
		if chunk == nil || chunk.len() != len(chunk.rowIDs) {
			return 0, false
		}
		for _, id := range chunk.rowIDs {
			if id == 0 || id >= ^RowID(0)-1 {
				return 0, false
			}
			if _, duplicate := seen[id]; duplicate {
				return 0, false
			}
			seen[id] = struct{}{}
			maxID = max(maxID, id)
		}
	}
	next := view.nextRowID
	if next == 0 {
		next = maxID + 1
	}
	return next, next > maxID && next < ^RowID(0)
}

// UnmarshalHistory accepts only VTC1. Validation precedes all decoded frame
// allocations, including validation of later chunks in the same payload.
func UnmarshalHistory(data []byte) (HistoryView, error) {
	if _, _, ok := parseHistory(data, false); !ok {
		return HistoryView{}, fmt.Errorf("unmarshal history: %w", errInvalidHistory)
	}
	view, _, ok := parseHistory(data, true)
	if !ok {
		return HistoryView{}, fmt.Errorf("unmarshal history: %w", errInvalidHistory)
	}
	return view, nil
}

func preflightHistory(data []byte) (historyDecodeStats, bool) {
	_, stats, ok := parseHistory(data, false)
	return stats, ok
}

func parseHistory(data []byte, populate bool) (HistoryView, historyDecodeStats, bool) {
	invalid := func() (HistoryView, historyDecodeStats, bool) { return HistoryView{}, historyDecodeStats{}, false }
	if len(data) < historyHeaderBytes || string(data[:4]) != historyMagic {
		return invalid()
	}
	p := historyParser{data: data[4:]}
	chunkCount, _ := p.uint32()
	nextID, _ := p.uint64()
	if chunkCount > maxHistoryChunks || uint64(chunkCount)*historyChunkHeaderBytes > uint64(len(p.data)) || nextID == 0 || nextID == math.MaxUint64 {
		return invalid()
	}
	stats := historyDecodeStats{chunks: uint64(chunkCount)}
	seenIDs := make(map[RowID]struct{})
	var maxID RowID
	var chunks []*HistoryChunk
	if populate {
		chunks = make([]*HistoryChunk, 0, chunkCount)
	}
	for range chunkCount {
		width, wok := p.uint32()
		rows, rok := p.uint32()
		nstyles, sok := p.uint32()
		if !wok || !rok || !sok || rows == 0 || rows > maxHistoryChunkRows || uint64(width) > uint64(math.MaxInt) || nstyles == 0 || nstyles > maxHistoryStylesPerChunk {
			return invalid()
		}
		cells := uint64(width) * uint64(rows)
		if uint64(nstyles) > cells+1 ||
			!addHistoryDecodeBudget(&stats.rows, uint64(rows), maxHistoryRows) ||
			!addHistoryDecodeBudget(&stats.cells, cells, maxHistoryCells) ||
			!addHistoryDecodeBudget(&stats.styles, uint64(nstyles), maxHistoryDecodeStyles) ||
			!addHistoryLogicalBytes(&stats, cells, uint64(rows), uint64(nstyles)) {
			return invalid()
		}
		// Counts have now passed aggregate budgets. This sum cannot overflow.
		payload := uint64(nstyles-1)*historyStyleBytes + uint64(rows)*historyRowBytes + cells*historyStoredCellBytes
		if payload > uint64(len(p.data)) {
			return invalid()
		}
		styles := make([]renderer.Style, nstyles)
		styles[0] = renderer.DefaultStyle()
		seenStyles := map[renderer.Style]struct{}{renderer.DefaultStyle(): {}}
		for id := uint32(1); id < nstyles; id++ {
			style, ok := p.style()
			if !ok || style != style.Canonical() || style.UnderlineStyle > renderer.UnderlineDashed {
				return invalid()
			}
			if _, duplicate := seenStyles[style]; duplicate {
				return invalid()
			}
			seenStyles[style] = struct{}{}
			styles[id] = style
		}
		rowIDs := make([]RowID, rows)
		bounds := make([]LineBound, rows)
		for row := range rows {
			id, ok := p.uint64()
			bound, bok := p.bound()
			if !ok || !bok || id == 0 || id >= math.MaxUint64-1 || !validHistoryBound(bound, int(width)) {
				return invalid()
			}
			if _, duplicate := seenIDs[RowID(id)]; duplicate {
				return invalid()
			}
			seenIDs[RowID(id)] = struct{}{}
			maxID = max(maxID, RowID(id))
			rowIDs[row], bounds[row] = RowID(id), bound
		}
		lastStyleRow := make([]int, nstyles)
		for i := range lastStyleRow {
			lastStyleRow[i] = -1
		}
		var frame renderer.Frame
		if populate {
			frame = renderer.NewFrame(int(width), int(rows))
		}
		for row := range rows {
			for x := range width {
				r, id, continuation, ok := p.storedCell(nstyles)
				if !ok {
					return invalid()
				}
				lastStyleRow[id] = int(row)
				if populate {
					frame.Set(int(x), int(row), renderer.Cell{Rune: r, Style: styles[id], Continuation: continuation})
				}
			}
		}
		for id := uint32(1); id < nstyles; id++ {
			if lastStyleRow[id] < 0 {
				return invalid()
			}
		}
		if populate {
			drops := make([]uint64, rows)
			for id := uint32(1); id < nstyles; id++ {
				drops[lastStyleRow[id]]++
			}
			chunks = append(chunks, &HistoryChunk{frame: frame, count: int(rows), width: int(width), bounds: bounds, rowIDs: rowIDs, styleDrops: drops, styleCount: uint64(nstyles)})
		}
	}
	if len(p.data) != 0 || RowID(nextID) <= maxID {
		return invalid()
	}
	return HistoryView{chunks: chunks, rows: int(stats.rows), cells: int(stats.cells), logicalBytes: stats.bytes, nextRowID: RowID(nextID)}, stats, true
}

func addHistoryLogicalBytes(stats *historyDecodeStats, cells, rows, styles uint64) bool {
	// Callers first bound all three counts by the aggregate decode ceilings.
	amount := cells*renderer.StoredCellLogicalBytes + rows*renderer.RowDescriptorLogicalBytes + styles*renderer.StyleRecordLogicalBytes
	return addHistoryDecodeBudget(&stats.bytes, amount, maxHistoryDecodedBytes)
}

func addHistoryDecodeBudget(total *uint64, amount, limit uint64) bool {
	if *total > limit || amount > limit-*total {
		return false
	}
	*total += amount
	return true
}

func validHistoryBound(bound LineBound, cells int) bool {
	return bound.End >= 0 && bound.End <= cells
}
