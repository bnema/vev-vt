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
	first := h.tailCells[:1][0:1]
	for i := 4; i < 8; i++ {
		require.NoError(t, h.Append(historyRow(fmt.Sprintf("r%03d", i)), LineBound{End: 4}))
	}
	require.Same(t, &first[0], &h.tailCells[:1][0], "storage reallocated across seals")
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

// Accounting after style/payload set reuse must match a history that has never
// sealed anything.
func TestHistoryLogicalBytesIndependentOfSetReuse(t *testing.T) {
	payload, err := renderer.NewCellPayload("x", "https://example.test")
	require.NoError(t, err)
	rows := make([][]renderer.Cell, 40)
	for i := range rows {
		rows[i] = styledRow("abcd", i%7+1)
		if i%3 == 0 {
			rows[i][1].Payload = payload
		}
	}
	reused := NewHistory(HistoryConfig{MaxRows: 100, MaxBytes: 1 << 20, ChunkRows: 5})
	whole := NewHistory(HistoryConfig{MaxRows: 100, MaxBytes: 1 << 20, ChunkRows: 100})
	for _, row := range rows {
		require.NoError(t, reused.Append(row, LineBound{End: 4}))
		require.NoError(t, whole.Append(row, LineBound{End: 4}))
	}
	// Re-encode each retained chunk from scratch and compare accounting.
	var fresh uint64
	for _, c := range reused.View().chunks {
		rebuilt := newHistoryChunks(c.rowsForTest(), c.bounds, c.rowIDs)
		fresh += historyChunksLogicalBytes(rebuilt)
	}
	require.Equal(t, fresh, reused.LogicalBytes())
	require.Equal(t, rangeTexts(t, whole.View()), rangeTexts(t, reused.View()))
}

func (c *HistoryChunk) rowsForTest() [][]renderer.Cell {
	rows := make([][]renderer.Cell, c.len())
	for i := range rows {
		rows[i] = c.row(i)
	}
	return rows
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
