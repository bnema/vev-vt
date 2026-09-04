package vt

import (
	"testing"

	"github.com/bnema/vev-vt/core"
	"github.com/stretchr/testify/require"
)

func TestHistoryViewReusesUnchangedTail(t *testing.T) {
	h := NewHistory(HistoryConfig{MaxRows: 8, ChunkRows: 4})
	require.NoError(t, h.Append([]core.Cell{{Rune: 'a', Style: core.DefaultStyle()}}, LineBound{End: 1}))
	first := h.View()
	require.Same(t, first.chunks[0], h.View().chunks[0])
	require.LessOrEqual(t, testing.AllocsPerRun(20, func() { _ = h.View() }), float64(1))
	require.Same(t, first.chunks[0], h.SealAndView().chunks[0])
}

func TestHistoryTailCacheInvalidationPreservesRetainedViews(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*History) error
		want   int
	}{
		{"append", func(h *History) error {
			return h.Append([]core.Cell{{Rune: 'c', Style: core.DefaultStyle()}}, LineBound{End: 1})
		}, 3},
		{"byte eviction", func(h *History) error {
			return h.SetLimits(HistoryConfig{MaxBytes: core.StoredCellLogicalBytes + core.RowDescriptorLogicalBytes + core.StyleRecordLogicalBytes, ChunkRows: 4})
		}, 1},
		{"disable", func(h *History) error { return h.SetLimits(HistoryConfig{}) }, 0},
		{"seal and row eviction", func(h *History) error { return h.SetLimits(HistoryConfig{MaxRows: 1, ChunkRows: 1}) }, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHistory(HistoryConfig{MaxRows: 8, ChunkRows: 4})
			for _, r := range "ab" {
				require.NoError(t, h.Append([]core.Cell{{Rune: r, Style: core.DefaultStyle()}}, LineBound{End: 1}))
			}
			before := h.View()
			snapshot := h.SnapshotView()
			require.NoError(t, tt.mutate(h))
			require.Equal(t, tt.want, h.View().Len())
			require.Equal(t, 2, before.Len())
			require.Equal(t, 'a', before.Cell(0, 0).Rune)
			require.Equal(t, 'b', before.Cell(0, 1).Rune)
			require.Equal(t, 'a', snapshot.Tail().Cell(0, 0).Rune)
			require.Equal(t, 'b', snapshot.Tail().Cell(0, 1).Rune)
		})
	}
}
