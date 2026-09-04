package vt

import renderer "github.com/bnema/vev-vt/core"

// RecoveryTranscriptSnapshot owns compact viewport pages to replay after
// retained history. No semantic row slices are retained by the snapshot.
type RecoveryTranscriptSnapshot struct {
	segments  []recoveryTranscriptSegment
	nextRowID RowID
}

type recoveryTranscriptSegment struct {
	frame  renderer.Frame
	bounds []LineBound
	rowIDs []RowID
}

// RecoveryTranscriptSnapshot captures the primary viewport, followed by the
// alternate viewport when active. Untouched trailing rows are omitted and each
// viewport ends with a hard line, independently of physical row rotation.
func (s *Screen) RecoveryTranscriptSnapshot() RecoveryTranscriptSnapshot {
	if s == nil {
		return RecoveryTranscriptSnapshot{}
	}
	buffers := []*buffer{s.buffer}
	if s.alternate != nil {
		buffers = []*buffer{s.alternate.buffer, s.buffer}
	}
	nextID := s.nextRowID
	if nextID < ^RowID(0) {
		nextID++
	}
	snapshot := RecoveryTranscriptSnapshot{nextRowID: nextID, segments: make([]recoveryTranscriptSegment, 0, len(buffers))}
	for _, b := range buffers {
		if b == nil {
			continue
		}
		rows := b.frame.Height
		for rows > 0 && recoveryTranscriptFrameRowUntouched(b.frame, rows-1, b.bound(rows-1)) {
			rows--
		}
		if rows == 0 {
			continue
		}
		frame := renderer.NewFrame(b.frame.Width, rows)
		for y := range rows {
			for x := range frame.Width {
				frame.Set(x, y, b.frame.Cell(x, y))
			}
		}
		bounds := append([]LineBound(nil), b.boundaries[:rows]...)
		bounds[rows-1].Soft = false
		snapshot.segments = append(snapshot.segments, recoveryTranscriptSegment{
			frame: frame, bounds: bounds, rowIDs: append([]RowID(nil), b.rowIDs[:rows]...),
		})
	}
	return snapshot
}

func recoveryTranscriptFrameRowUntouched(frame renderer.Frame, y int, bound LineBound) bool {
	if bound.End != 0 || bound.Soft {
		return false
	}
	blank := renderer.BlankCell()
	for x := range frame.Width {
		if !frame.Cell(x, y).Equal(blank) {
			return false
		}
	}
	return true
}

// Marshal encodes the compact capture without consulting or mutating live state.
func (snapshot RecoveryTranscriptSnapshot) Marshal() ([]byte, error) {
	view := HistoryView{nextRowID: snapshot.nextRowID}
	for _, segment := range snapshot.segments {
		for start := 0; start < segment.frame.Height; {
			count := min(segment.frame.Height-start, maxHistoryChunkRows, maxHistorySlabRows(segment.frame.Width))
			frame := renderer.NewFrame(segment.frame.Width, count)
			lastStyleRow := make(map[renderer.Style]int)
			for y := range count {
				for x := range frame.Width {
					cell := segment.frame.Cell(x, start+y)
					frame.Set(x, y, cell)
					style := cell.Style.Canonical()
					if style != renderer.DefaultStyle() {
						lastStyleRow[style] = y
					}
				}
			}
			drops := make([]uint64, count)
			for _, y := range lastStyleRow {
				drops[y]++
			}
			chunk := &HistoryChunk{
				frame: frame, count: count, width: frame.Width,
				bounds: segment.bounds[start : start+count], rowIDs: segment.rowIDs[start : start+count],
				styleDrops: drops, styleCount: uint64(len(lastStyleRow)) + 1,
			}
			chunk.recordPayloads()
			view.chunks = append(view.chunks, chunk)
			view.rows += count
			view.cells += count * frame.Width
			view.logicalBytes += historyChunkLogicalBytes(chunk)
			start += count
		}
	}
	return MarshalHistory(view)
}
