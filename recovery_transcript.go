package vt

import renderer "github.com/bnema/vev-vt/core"

// RecoveryTranscriptSnapshot is an owned, immutable capture of the viewport
// rows that should be replayed after retained terminal history during recovery.
type RecoveryTranscriptSnapshot struct {
	segments  []recoveryTranscriptSegment
	nextRowID RowID
}

type recoveryTranscriptSegment struct {
	rows   [][]renderer.Cell
	bounds []LineBound
	rowIDs []RowID
}

// RecoveryTranscriptSnapshot captures the active primary viewport, or the
// saved primary viewport followed by the active alternate viewport.
func (s *Screen) RecoveryTranscriptSnapshot() RecoveryTranscriptSnapshot {
	if s == nil {
		return RecoveryTranscriptSnapshot{}
	}

	buffers := []*buffer{s.buffer}
	if s.alternate != nil {
		buffers = []*buffer{s.alternate.buffer, s.buffer}
	}

	nextRowID := s.nextRowID
	if nextRowID < ^RowID(0) {
		nextRowID++
	}
	snapshot := RecoveryTranscriptSnapshot{segments: make([]recoveryTranscriptSegment, 0, len(buffers)), nextRowID: nextRowID}
	for _, b := range buffers {
		segment := captureRecoveryTranscriptSegment(b)
		if len(segment.rows) > 0 {
			snapshot.segments = append(snapshot.segments, segment)
		}
	}
	return snapshot
}

func captureRecoveryTranscriptSegment(b *buffer) recoveryTranscriptSegment {
	if b == nil {
		return recoveryTranscriptSegment{}
	}

	rowCount := b.frame.Height
	for rowCount > 0 && recoveryTranscriptFrameRowUntouched(b.frame, rowCount-1, b.bound(rowCount-1)) {
		rowCount--
	}
	if rowCount == 0 {
		return recoveryTranscriptSegment{}
	}

	segment := recoveryTranscriptSegment{
		rows:   make([][]renderer.Cell, rowCount),
		bounds: append([]LineBound(nil), b.boundaries[:rowCount]...),
		rowIDs: append([]RowID(nil), b.rowIDs[:rowCount]...),
	}
	cells := make([]renderer.Cell, rowCount*b.frame.Width)
	for y := range rowCount {
		start := y * b.frame.Width
		end := start + b.frame.Width
		segment.rows[y] = cells[start:end:end]
		for x := range b.frame.Width {
			segment.rows[y][x] = b.frame.At(x, y)
		}
	}
	segment.bounds[rowCount-1].Soft = false
	return segment
}

func recoveryTranscriptFrameRowUntouched(frame renderer.Frame, y int, bound LineBound) bool {
	if bound.End != 0 || bound.Soft {
		return false
	}
	blank := renderer.BlankCell()
	for x := range frame.Width {
		if !frame.At(x, y).Equal(blank) {
			return false
		}
	}
	return true
}

// Marshal encodes the captured rows as canonical terminal history without
// consulting or mutating live Screen history.
func (snapshot RecoveryTranscriptSnapshot) Marshal() ([]byte, error) {
	rows := 0
	cells := 0
	for _, segment := range snapshot.segments {
		rows += len(segment.rows)
		for _, row := range segment.rows {
			cells += len(row)
		}
	}

	view := HistoryView{rows: rows, cells: cells, nextRowID: snapshot.nextRowID}
	var allRows [][]renderer.Cell
	var allBounds []LineBound
	var allIDs []RowID
	for _, segment := range snapshot.segments {
		allRows = append(allRows, segment.rows...)
		allBounds = append(allBounds, segment.bounds...)
		allIDs = append(allIDs, segment.rowIDs...)
	}
	for start := 0; start < len(allRows); start += maxHistoryChunkRows {
		end := min(start+maxHistoryChunkRows, len(allRows))
		view.chunks = append(view.chunks, newHistoryChunks(allRows[start:end], allBounds[start:end], allIDs[start:end])...)
	}
	return MarshalHistory(view)
}
