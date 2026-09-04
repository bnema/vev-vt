package vt

import (
	"fmt"
	"math"
)

// DecodeStats describes resources declared by one canonical VT blob.
type DecodeStats struct {
	Chunks uint64
	Rows   uint64
	Cells  uint64
	Styles uint64
	Bytes  uint64
}

// Add adds another preflight result with overflow checking.
func (s *DecodeStats) Add(other DecodeStats) bool {
	for _, totals := range [][2]*uint64{{&s.Chunks, &other.Chunks}, {&s.Rows, &other.Rows}, {&s.Cells, &other.Cells}, {&s.Styles, &other.Styles}, {&s.Bytes, &other.Bytes}} {
		if math.MaxUint64-*totals[0] < *totals[1] {
			return false
		}
		*totals[0] += *totals[1]
	}
	return true
}

// PreflightHistoryBlob validates one self-contained history blob without
// allocating decoded rows.
func PreflightHistoryBlob(data []byte) (DecodeStats, error) {
	stats, ok := preflightHistory(data)
	if !ok {
		return DecodeStats{}, fmt.Errorf("preflight history: %w", errInvalidHistory)
	}
	return DecodeStats{Chunks: stats.chunks, Rows: stats.rows, Cells: stats.cells, Styles: stats.styles, Bytes: stats.bytes}, nil
}

// MarshalHistoryChunk encodes one immutable chunk as a self-contained blob.
func MarshalHistoryChunk(chunk *HistoryChunk) ([]byte, error) {
	if chunk == nil {
		return nil, fmt.Errorf("marshal history chunk: %w", errInvalidHistory)
	}
	return MarshalHistory(HistoryView{chunks: []*HistoryChunk{chunk}, rows: chunk.len(), cells: chunk.cells()})
}

// MarshalEmptyHistoryTail returns the mandatory canonical empty tail blob.
func MarshalEmptyHistoryTail() ([]byte, error) { return MarshalHistory(HistoryView{}) }

// MarshalHistoryTail encodes the copied mutable tail of a snapshot view as one
// canonical history blob. It does not seal or otherwise mutate live history.
func MarshalHistoryTail(view HistorySnapshotView) ([]byte, error) {
	return MarshalHistory(view.Tail())
}

// MarshalSealedHistory serializes a SealAndView result as oldest-first,
// self-contained sealed blobs plus a mandatory empty tail blob. The empty tail
// carries NextRowID and is not the canonical default empty encoding.
func MarshalSealedHistory(view HistoryView) ([][]byte, []byte, error) {
	if view.rows != historyViewRowCount(view) {
		return nil, nil, fmt.Errorf("marshal sealed history: %w", errInvalidHistory)
	}
	sealed := make([][]byte, len(view.chunks))
	for i, chunk := range view.chunks {
		blob, err := MarshalHistoryChunk(chunk)
		if err != nil {
			return nil, nil, err
		}
		sealed[i] = blob
	}
	tail, err := MarshalHistory(HistoryView{nextRowID: view.NextRowID()})
	if err != nil {
		return nil, nil, err
	}
	return sealed, tail, nil
}

// HistoryFromBlobs restores history directly from sealed, oldest-first blobs
// and the mandatory tail blob. It never feeds decoded rows through Append.
func HistoryFromBlobs(config HistoryConfig, sealed [][]byte, tail []byte) (*History, error) {
	sealedViews, tailView, err := decodeRestoredHistoryBlobs(sealed, tail)
	if err != nil {
		return nil, err
	}
	return restoreHistoryViews(config, sealedViews, tailView), nil
}

func decodeRestoredHistoryBlobs(sealed [][]byte, tail []byte, extra ...HistoryView) ([]HistoryView, HistoryView, error) {
	seen := make(map[RowID]struct{})
	sealedViews := make([]HistoryView, len(sealed))
	for i, blob := range sealed {
		view, err := UnmarshalHistory(blob)
		if err != nil || len(view.chunks) != 1 || view.chunks[0].len() == 0 || !validateRestoredHistoryView(view, seen) {
			return nil, HistoryView{}, fmt.Errorf("restore sealed history: %w", errInvalidHistory)
		}
		sealedViews[i] = view
	}

	tailView, err := UnmarshalHistory(tail)
	if err != nil || !validateRestoredHistoryView(tailView, seen) {
		return nil, HistoryView{}, fmt.Errorf("restore history tail: %w", errInvalidHistory)
	}
	for _, view := range extra {
		if !validateRestoredHistoryView(view, seen) {
			return nil, HistoryView{}, fmt.Errorf("restore recovery transcript: %w", errInvalidHistory)
		}
	}
	return sealedViews, tailView, nil
}

func restoreHistoryViews(config HistoryConfig, sealed []HistoryView, tail HistoryView) *History {
	h := NewHistory(config)
	for _, view := range sealed {
		h.nextRowID = max(h.nextRowID, view.nextRowID)
		chunk := h.boundedRestoredChunk(view.chunks[0])
		if chunk == nil {
			continue
		}
		chunkBytes := historyChunkLogicalBytes(chunk)
		for h.rows > h.maxRows-chunk.len() || h.logicalBytes > h.maxBytes-chunkBytes {
			h.evictOldest()
		}
		h.chunks = append(h.chunks, chunk)
		h.rows += chunk.len()
		h.cells += chunk.cells()
		h.logicalBytes += chunkBytes
	}
	h.nextRowID = max(h.nextRowID, tail.nextRowID)
	if h.maxRows == 0 {
		return h
	}
	for _, chunk := range tail.chunks {
		for row := range chunk.len() {
			if h.prepareChunkRowStyles(chunk, row) > h.maxBytes {
				continue
			}
			for {
				delta := h.tailAppendDelta(chunk.width)
				if h.rows < h.maxRows && h.logicalBytes <= h.maxBytes-delta {
					h.appendTailChunkRow(chunk, row)
					h.recordTailRowStyles(chunk.width)
					h.tailBytes += delta
					h.logicalBytes += delta
					h.tailBounds = append(h.tailBounds, chunk.bounds[row])
					h.tailIDs = append(h.tailIDs, chunk.rowIDs[row])
					h.rows++
					h.cells += chunk.width
					if len(h.tail) == h.chunkRows {
						h.sealTail()
					}
					break
				}
				h.evictOldest()
				h.prepareChunkRowStyles(chunk, row)
			}
		}
	}
	return h
}

func (h *History) boundedRestoredChunk(chunk *HistoryChunk) *HistoryChunk {
	if h.maxRows == 0 || chunk == nil {
		return nil
	}
	bounded := chunk
	for bounded.count > h.maxRows || historyChunkLogicalBytes(bounded) > h.maxBytes {
		if bounded.count == 1 {
			return nil
		}
		bounded = &HistoryChunk{
			frame:      bounded.frame,
			start:      bounded.start + 1,
			count:      bounded.count - 1,
			width:      bounded.width,
			bounds:     bounded.bounds[1:],
			rowIDs:     bounded.rowIDs[1:],
			styleDrops: bounded.styleDrops,
			styleCount: bounded.styleCount - bounded.styleDrops[bounded.start],
		}
	}
	return bounded
}

func validateRestoredHistoryView(view HistoryView, seen map[RowID]struct{}) bool {
	if view.nextRowID == 0 || view.nextRowID == ^RowID(0) {
		return false
	}
	var maxID RowID
	for _, chunk := range view.chunks {
		if chunk == nil || chunk.len() != len(chunk.rowIDs) || chunk.len() != len(chunk.bounds) {
			return false
		}
		for _, id := range chunk.rowIDs {
			if id == 0 || id >= ^RowID(0)-1 {
				return false
			}
			if _, duplicate := seen[id]; duplicate {
				return false
			}
			seen[id] = struct{}{}
			maxID = max(maxID, id)
		}
	}
	return view.nextRowID > maxID
}

// NewScreenWithRecoveryTranscript constructs a fresh blank screen whose
// history contains the restored bounded history followed by the recovery
// transcript. The transcript is decoded in full before history is restored.
func NewScreenWithRecoveryTranscript(width, height int, config HistoryConfig, sealed [][]byte, tail, transcript []byte) (*Screen, error) {
	transcriptView, err := UnmarshalHistory(transcript)
	if err != nil {
		return nil, fmt.Errorf("restore recovery transcript: %w", err)
	}

	sealedViews, tailView, err := decodeRestoredHistoryBlobs(sealed, tail, transcriptView)
	if err != nil {
		return nil, err
	}
	history := restoreHistoryViews(config, sealedViews, tailView)
	remainingRows := transcriptView.rows
	for _, chunk := range transcriptView.chunks {
		for i := range chunk.len() {
			row := chunk.row(i)
			remainingRows--
			bound := chunk.bounds[i]
			if remainingRows == 0 {
				bound.Soft = false
			}
			if err := history.AppendWithID(row, bound, chunk.rowIDs[i]); err != nil {
				return nil, fmt.Errorf("restore recovery transcript: %w", err)
			}
		}
	}

	screen := NewScreenWithHistory(width, height, config)
	screen.history = history
	counter := max(history.nextRowID, transcriptView.nextRowID, RowID(1))
	if counter == 0 || counter >= ^RowID(0) || uint64(height) > uint64(^RowID(0)-1-counter) {
		return nil, fmt.Errorf("restore recovery transcript: %w", errInvalidHistory)
	}
	screen.nextRowID = counter - 1
	for i := range screen.buffer.rowIDs {
		screen.buffer.rowIDs[i] = screen.nextRowIDValue()
	}
	screen.frame = screen.buffer.frame
	return screen, nil
}
