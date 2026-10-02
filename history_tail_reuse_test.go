package vt

import (
	"fmt"
	"math/rand"
	"testing"

	renderer "github.com/bnema/vev-vt/core"
	"github.com/stretchr/testify/require"
)

func styledRow(text string, fg int) []renderer.Cell {
	row := historyRow(text)
	for i := range row {
		row[i].Style = renderer.Style{Bold: true, Foreground: fg}
	}
	return row
}

func rangeTexts(t *testing.T, v HistoryView) []string {
	t.Helper()
	var out []string
	require.NoError(t, v.Range(func(row []renderer.Cell) bool {
		out = append(out, rowText(row))
		return true
	}))
	return out
}

// Sealing recycles the private tail storage. No previously handed-out view,
// snapshot tail or Range row may observe later appends or seals.
func TestHistoryTailStorageReuseKeepsViewsStable(t *testing.T) {
	h := NewHistory(HistoryConfig{MaxRows: 64, MaxBytes: 1 << 20, ChunkRows: 4})
	appendRow := func(i int) {
		require.NoError(t, h.Append(styledRow(fmt.Sprintf("r%03d", i), i%5+1), LineBound{End: 4}))
	}
	for i := range 2 {
		appendRow(i)
	}
	partial := h.View()
	snap := h.SnapshotView()
	var ranged [][]renderer.Cell
	require.NoError(t, partial.Range(func(row []renderer.Cell) bool { ranged = append(ranged, row); return true }))

	for i := 2; i < 14; i++ { // seals three times, reusing storage each time
		appendRow(i)
	}
	sealedView := h.SealAndView()
	appendRow(14)
	appendRow(15)

	require.Equal(t, []string{"r000", "r001"}, rangeTexts(t, partial))
	require.Equal(t, []string{"r000", "r001"}, rangeTexts(t, snap.Tail()))
	require.Equal(t, "r000", rowText(ranged[0]))
	require.Equal(t, "r001", rowText(ranged[1]))
	require.Equal(t, 14, sealedView.Len())
	texts := rangeTexts(t, sealedView)
	for i, got := range texts {
		require.Equal(t, fmt.Sprintf("r%03d", i), got)
	}
	live := rangeTexts(t, h.View())
	require.Len(t, live, 16)
	require.Equal(t, "r015", live[15])
	for _, c := range h.chunks {
		require.NoError(t, c.CheckInvariants())
	}
}

func TestHistoryTailStorageIsRecycledAcrossSeals(t *testing.T) {
	h := NewHistory(HistoryConfig{MaxRows: 64, MaxBytes: 1 << 20, ChunkRows: 4})
	for i := range 4 {
		require.NoError(t, h.Append(historyRow(fmt.Sprintf("r%03d", i)), LineBound{End: 4}))
	}
	require.Empty(t, h.tail)
	require.Zero(t, len(h.tailCells))
	require.NotZero(t, cap(h.tailCells), "seal must keep tail storage for the next chunk")
	first := &h.tailCells[:1][0]
	for i := 4; i < 8; i++ {
		require.NoError(t, h.Append(historyRow(fmt.Sprintf("r%03d", i)), LineBound{End: 4}))
	}
	require.Same(t, first, &h.tailCells[:1][0], "storage reallocated across seals")
	// A reused backing array must not pin payload strings of sealed rows.
	payload, err := renderer.NewCellPayload("é\u0301", "https://example.test")
	require.NoError(t, err)
	row := historyRow("abcd")
	row[0].Payload = payload
	for range 4 {
		require.NoError(t, h.Append(row, LineBound{End: 4}))
	}
	for _, c := range h.tailCells[:cap(h.tailCells)] {
		require.True(t, c.Payload.Empty())
	}
	require.NoError(t, h.chunks[len(h.chunks)-1].CheckInvariants())
}

// Default chunks of wide terminals (256 rows × 200 columns) must recycle their
// tail storage too.
func TestHistoryTailStorageIsRecycledForWideTerminals(t *testing.T) {
	const width = 200
	h := NewHistory(HistoryConfig{MaxRows: 4096, MaxBytes: 1 << 30})
	row := make([]renderer.Cell, width)
	for i := range row {
		row[i] = renderer.Cell{Rune: 'w', Style: renderer.DefaultStyle()}
	}
	for range 256 {
		require.NoError(t, h.Append(row, LineBound{End: width}))
	}
	require.Empty(t, h.tail)
	require.GreaterOrEqual(t, cap(h.tailCells), 256*width, "seal released wide-terminal tail storage")
}

// Eviction from a partial tail must not leave the evicted rows' payload
// strings pinned in the recycled backing array's prefix.
func TestHistoryTailEvictionClearsEvictedPrefix(t *testing.T) {
	payload, err := renderer.NewCellPayload("é\u0301", "https://example.test")
	require.NoError(t, err)
	rowFor := func(i int) []renderer.Cell {
		row := historyRow("abcd")
		row[i%4].Payload = payload
		return row
	}
	// The byte budget evicts from the tail (ChunkRows is large, so it never
	// seals); the 400-cell preallocation leaves room for many evictions in
	// one backing array before it would be reallocated.
	h := NewHistory(HistoryConfig{MaxRows: 100, MaxBytes: 1500, ChunkRows: 100})
	require.NoError(t, h.Append(rowFor(0), LineBound{End: 4}))
	orig := h.tailCells[:cap(h.tailCells)] // full original backing array
	evicted := 0
	for i := 1; i < 40 && len(h.chunks) == 0; i++ {
		before := h.Len()
		require.NoError(t, h.Append(rowFor(i), LineBound{End: 4}))
		if cap(h.tailCells) > len(orig) || len(h.tailCells) == 0 {
			break
		}
		liveStart := len(orig) - cap(h.tailCells)
		if &orig[liveStart] != &h.tailCells[:1][0] {
			break // storage was reallocated; the old array is no longer ours
		}
		if h.Len() <= before {
			evicted++
		}
		liveEnd := liveStart + len(h.tailCells)
		for j, c := range orig {
			if j < liveStart || j >= liveEnd {
				require.True(t, c.Payload.Empty(), "cell %d outside live window [%d,%d) pins payload", j, liveStart, liveEnd)
			}
		}
	}
	require.GreaterOrEqual(t, evicted, 2, "test did not exercise tail eviction")
}

// checkHistoryAccounting compares the incrementally maintained LogicalBytes
// with (1) the sum of the live chunks' own metadata and (2) an independent
// recomputation: every live chunk is rebuilt from its decoded rows with a
// fresh newHistoryChunks call (no recycled scratch state). Logical bytes depend
// on the page layout, so the reference keeps each chunk's boundaries.
func checkHistoryAccounting(t *testing.T, h *History) {
	t.Helper()
	view := h.View()
	require.Equal(t, historyChunksLogicalBytes(view.chunks), h.LogicalBytes(), "rows=%d", h.Len())
	require.Equal(t, h.Len(), view.Len())
	var rebuilt uint64
	for _, c := range view.chunks {
		rows := make([][]renderer.Cell, c.len())
		for i := range rows {
			rows[i] = c.row(i)
		}
		rebuilt += historyChunksLogicalBytes(newHistoryChunks(rows, c.bounds, c.rowIDs))
	}
	require.Equal(t, rebuilt, h.LogicalBytes(), "independent rebuild rows=%d", h.Len())
}

// Accounting must stay exact after seals recycle the style and payload sets,
// through eviction, SetLimits shrinking, payload rows, width changes and pages
// with more than maxRetainedStyleScratch distinct styles.
func TestHistoryLogicalBytesIndependentOfSetReuse(t *testing.T) {
	payload, err := renderer.NewCellPayload("x", "https://example.test")
	require.NoError(t, err)
	payload2, err := renderer.NewCellPayload("y", "")
	require.NoError(t, err)
	manyStyles := func(i int) []renderer.Cell { // 1100 distinct styles in one row
		row := make([]renderer.Cell, 1100)
		for x := range row {
			row[x] = renderer.Cell{Rune: 'z', Style: renderer.Style{HasForegroundRGB: true, ForegroundRGB: renderer.RGB{R: uint8(x), G: uint8(x >> 8), B: uint8(i)}}}
		}
		return row
	}
	rowFor := func(i int) []renderer.Cell {
		switch {
		case i%11 == 5:
			return manyStyles(i)
		case i%4 == 0:
			row := styledRow("abcd", i%7+1)
			row[1].Payload = payload
			row[2].Payload = payload2
			return row
		case i%3 == 0:
			return styledRow("abcdef", i%5+1) // width change forces a new page
		}
		return styledRow("abcd", i%7+1)
	}
	for _, tc := range []struct {
		name string
		cfg  HistoryConfig
	}{
		{"non-divisible chunks", HistoryConfig{MaxRows: 200, MaxBytes: 1 << 30, ChunkRows: 7}},
		{"row eviction", HistoryConfig{MaxRows: 9, MaxBytes: 1 << 30, ChunkRows: 4}},
		{"byte eviction", HistoryConfig{MaxRows: 200, MaxBytes: 30 << 10, ChunkRows: 5}},
		{"tiny bytes", HistoryConfig{MaxRows: 200, MaxBytes: 1500, ChunkRows: 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHistory(tc.cfg)
			appendAt := func(i int) {
				row := rowFor(i)
				if err := h.Append(row, LineBound{End: len(row)}); err != nil {
					require.ErrorIs(t, err, ErrHistoryRowTooLarge)
				}
				checkHistoryAccounting(t, h)
			}
			for i := range 60 {
				appendAt(i)
			}
			shrunk := HistoryConfig{MaxRows: 5, MaxBytes: tc.cfg.MaxBytes, ChunkRows: 2}
			require.NoError(t, h.SetLimits(shrunk))
			checkHistoryAccounting(t, h)
			for i := 60; i < 75; i++ {
				appendAt(i)
			}
		})
	}
}

// rowIDSet must accept and reject exactly what a plain map would, including
// out-of-order IDs and duplicates across commits.
func TestRowIDSetMatchesMap(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for iter := 0; iter < 5000; iter++ {
		var set rowIDSet
		ref := map[RowID]struct{}{}
		for commit := 0; commit < 1+rng.Intn(4); commit++ {
			n := rng.Intn(6)
			ids := make([]RowID, n)
			base := RowID(rng.Intn(30))
			for i := range ids {
				if rng.Intn(3) == 0 {
					ids[i] = RowID(1 + rng.Intn(30))
				} else {
					ids[i] = base + RowID(i) + 1
				}
			}
			if rng.Intn(4) == 0 && n > 1 {
				ids[0], ids[n-1] = ids[n-1], ids[0]
			}
			want := true
			seen := map[RowID]struct{}{}
			for _, id := range ids {
				if _, dup := ref[id]; dup {
					want = false
				}
				if _, dup := seen[id]; dup {
					want = false
				}
				seen[id] = struct{}{}
			}
			set.reserve(n)
			for _, id := range ids {
				set.collect(id)
			}
			require.Equal(t, want, set.commit(), "iter %d ids %v", iter, ids)
			if !want {
				break
			}
			for _, id := range ids {
				ref[id] = struct{}{}
			}
		}
	}
}

func TestRestoreAcceptsOutOfOrderDistinctRowIDsAcrossBlobs(t *testing.T) {
	src := NewHistory(HistoryConfig{MaxRows: 16, ChunkRows: 2})
	for _, id := range []RowID{100, 7, 8, 9} {
		require.NoError(t, src.AppendWithID(historyRow("abcd"), LineBound{End: 4}, id))
	}
	sealed, tail, err := MarshalSealedHistory(src.SealAndView())
	require.NoError(t, err)
	restored, err := HistoryFromBlobs(HistoryConfig{MaxRows: 16, ChunkRows: 2}, sealed, tail)
	require.NoError(t, err)
	require.Equal(t, 4, restored.Len())
	// Same IDs in two blobs are still rejected, in either order.
	_, err = HistoryFromBlobs(HistoryConfig{MaxRows: 16, ChunkRows: 2}, [][]byte{sealed[0], sealed[0]}, tail)
	require.Error(t, err)
	_, err = HistoryFromBlobs(HistoryConfig{MaxRows: 16, ChunkRows: 2}, [][]byte{sealed[1], sealed[0], sealed[1]}, tail)
	require.Error(t, err)
}

// A chunk with more than maxRetainedStyleScratch styles must not leave its
// large scratch map pinned, whether it is the last or an earlier chunk.
func TestHistoryStyleScratchDroppedAfterHighCardinalityChunk(t *testing.T) {
	row := make([]renderer.Cell, 1100)
	for x := range row {
		row[x] = renderer.Cell{Rune: 'z', Style: renderer.Style{HasForegroundRGB: true, ForegroundRGB: renderer.RGB{R: uint8(x), G: uint8(x >> 8), B: 1}}}
	}
	h := NewHistory(HistoryConfig{MaxRows: 100, MaxBytes: 1 << 30, ChunkRows: 4})
	for range 4 { // seals one high-cardinality chunk
		require.NoError(t, h.Append(row, LineBound{End: len(row)}))
	}
	require.LessOrEqual(t, len(h.styleRowScratch), maxRetainedStyleScratch)
	require.LessOrEqual(t, len(h.tailPageStyles), maxRetainedStyleScratch+1)
	for range 3 {
		require.NoError(t, h.Append(historyRow("abcd"), LineBound{End: 4}))
	}
	_ = h.View()
	require.LessOrEqual(t, len(h.styleRowScratch), maxRetainedStyleScratch)
}

// Early false returns from validation must not leak collected IDs into a later
// commit.
func TestRowIDSetBeginDiscardsUncommittedIDs(t *testing.T) {
	var set rowIDSet
	set.begin(0)
	set.collect(1)
	set.collect(2) // abandoned without commit, as on an early invalid return
	set.begin(0)
	set.collect(2)
	require.True(t, set.commit())
	set.begin(0)
	set.collect(1)
	require.True(t, set.commit())
}
