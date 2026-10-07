package vt

import (
	"fmt"
	"testing"

	renderer "github.com/bnema/vev-vt/core"
	"github.com/stretchr/testify/require"
)

// Eviction advances an uncaptured head wrapper in place. A view or snapshot
// taken between evictions must keep its rows, accounting, and IDs.
func TestHistoryInPlaceEvictionKeepsCapturedViewsStable(t *testing.T) {
	tests := []struct {
		name    string
		capture func(*History) func() ([]string, int, uint64, RowID)
	}{
		{name: "view", capture: func(h *History) func() ([]string, int, uint64, RowID) {
			v := h.View()
			return func() ([]string, int, uint64, RowID) {
				return rangeTextsNoT(v), v.Len(), v.LogicalBytes(), v.RowID(0)
			}
		}},
		{name: "snapshot", capture: func(h *History) func() ([]string, int, uint64, RowID) {
			s := h.SnapshotView()
			return func() ([]string, int, uint64, RowID) {
				var texts []string
				for i := range s.ChunkCount() {
					c := s.Chunk(i)
					for r := range c.len() {
						texts = append(texts, rowText(c.row(r)))
					}
				}
				texts = append(texts, rangeTextsNoT(s.Tail())...)
				var first RowID
				if s.ChunkCount() > 0 {
					first = s.Chunk(0).rowIDs[0]
				}
				return texts, s.Len(), s.LogicalBytes(), first
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHistory(HistoryConfig{MaxRows: 12, MaxBytes: 1 << 20, ChunkRows: 4})
			add := func(i int) {
				require.NoError(t, h.Append(styledRow(fmt.Sprintf("r%03d", i), i%3+1), LineBound{End: 4}))
			}
			for i := range 14 { // two evictions: the head wrapper is now private
				add(i)
			}
			read := tt.capture(h)
			texts, rows, bytes, first := read()
			for i := 14; i < 30; i++ { // evicts through the captured head and beyond
				add(i)
			}
			gotTexts, gotRows, gotBytes, gotFirst := read()
			require.Equal(t, texts, gotTexts)
			require.Equal(t, rows, gotRows)
			require.Equal(t, bytes, gotBytes)
			require.Equal(t, first, gotFirst)
			require.Equal(t, 12, h.Len())
			require.Equal(t, "r018", rangeTextsNoT(h.View())[0])
		})
	}
}

// In-place eviction keeps logical accounting identical to wrapper replacement.
func TestHistoryInPlaceEvictionAccountingMatchesFreshBuild(t *testing.T) {
	h := NewHistory(HistoryConfig{MaxRows: 10, MaxBytes: 1 << 20, ChunkRows: 4})
	for i := range 37 {
		require.NoError(t, h.Append(styledRow(fmt.Sprintf("r%03d", i), i%5+1), LineBound{End: 4}))
	}
	view := h.View()
	for i := range view.ChunkCount() {
		require.NoError(t, view.Chunk(i).CheckInvariants())
	}
	var sum uint64
	for i := range view.ChunkCount() {
		sum += historyChunkLogicalBytes(view.Chunk(i))
	}
	require.Equal(t, sum, h.LogicalBytes())
}

// An idle pass releases spare mutable-tail capacity without changing content,
// and later appends regrow it.
func TestHistoryCompressIdleTrimsTailCapacity(t *testing.T) {
	for _, tailRows := range []int{0, 3} {
		t.Run(fmt.Sprintf("tail-%d", tailRows), func(t *testing.T) {
			h := NewHistory(HistoryConfig{MaxRows: 64, MaxBytes: 1 << 20, ChunkRows: 8})
			for i := range 16 + tailRows {
				require.NoError(t, h.Append(styledRow(fmt.Sprintf("r%03d", i), i%4+1), LineBound{End: 4}))
			}
			before := rangeTextsNoT(h.View())
			bytes := h.LogicalBytes()
			_, err := h.CompressIdle(1)
			require.NoError(t, err)
			require.Equal(t, len(h.tailCells), cap(h.tailCells))
			require.Equal(t, before, rangeTextsNoT(h.View()))
			require.Equal(t, bytes, h.LogicalBytes())
			require.NoError(t, h.Append(styledRow("next", 1), LineBound{End: 4}))
			got := rangeTextsNoT(h.View())
			require.Equal(t, append(before, "next"), got)
		})
	}
}

func TestStyleSet(t *testing.T) {
	style := func(i int) renderer.Style { return renderer.Style{Foreground: i % 256, Bold: i >= 256} }
	tests := []struct {
		name  string
		input []int
		want  int
	}{
		{name: "empty", want: 0},
		{name: "duplicates", input: []int{1, 1, 2, 2, 1}, want: 2},
		{name: "inline capacity", input: seqInts(styleSetInline), want: styleSetInline},
		{name: "spills to map", input: append(seqInts(styleSetInline+5), 0, 1, 2), want: styleSetInline + 5},
		{name: "large", input: seqInts(400), want: 400},
	}
	var s styleSet
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s.reset()
			want := map[renderer.Style]struct{}{}
			for _, i := range tt.input {
				s.add(style(i))
				want[style(i)] = struct{}{}
			}
			require.Equal(t, tt.want, s.len())
			got := map[renderer.Style]struct{}{}
			for st := range s.all() {
				_, dup := got[st]
				require.False(t, dup, "style yielded twice")
				got[st] = struct{}{}
			}
			require.Equal(t, want, got)
		})
	}
}

func seqInts(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func rangeTextsNoT(v HistoryView) []string {
	var out []string
	_ = v.Range(func(row []renderer.Cell) bool {
		out = append(out, rowText(row))
		return true
	})
	return out
}
