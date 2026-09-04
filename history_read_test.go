package vt

import (
	"testing"

	renderer "github.com/bnema/vev-vt/core"
	"github.com/stretchr/testify/require"
)

func TestHistoryScalarReadsAndCopyDoNotAllocate(t *testing.T) {
	h := NewHistory(HistoryConfig{MaxRows: 4, ChunkRows: 1})
	require.NoError(t, h.Append(historyRow("abc"), LineBound{End: 3}))
	require.NoError(t, h.Append(historyRow("de"), LineBound{End: 2}))
	view := h.View()
	require.Equal(t, 3, view.RowWidth(0))
	require.Equal(t, 2, view.RowWidth(1))
	for _, y := range []int{-1, 2} {
		require.Zero(t, view.RowWidth(y))
		require.Equal(t, renderer.BlankCell(), view.Cell(0, y))
	}
	require.Equal(t, renderer.BlankCell(), view.Cell(-1, 0))
	require.Equal(t, renderer.BlankCell(), view.Cell(3, 0))
	dst := make([]renderer.Cell, 2)
	require.Equal(t, 2, view.CopyRow(0, dst))
	require.Equal(t, 'a', dst[0].Rune)
	dst[0].Rune = 'X'
	require.Equal(t, 'a', view.Cell(0, 0).Rune)
	require.Zero(t, testing.AllocsPerRun(100, func() {
		_ = view.RowWidth(1)
		_ = view.Cell(1, 1)
		view.CopyRow(0, dst)
	}))
}

func TestSnapshotTailReusesItsCompactCapture(t *testing.T) {
	h := NewHistory(HistoryConfig{MaxRows: 4, ChunkRows: 4})
	require.NoError(t, h.Append(historyRow("ab"), LineBound{End: 2}))
	snapshot := h.SnapshotView()
	tail := snapshot.Tail()
	require.Same(t, tail.Chunk(0), snapshot.Tail().Chunk(0))
	require.Zero(t, testing.AllocsPerRun(100, func() { _ = snapshot.Tail() }))
	require.NoError(t, h.Append(historyRow("cd"), LineBound{End: 2}))
	require.Equal(t, 1, tail.Len())
	require.Equal(t, 'a', tail.Cell(0, 0).Rune)
	require.NoError(t, tail.Chunk(0).CheckInvariants())
}

func BenchmarkRecoveryTranscriptCompactCapture(b *testing.B) {
	s := NewScreen(120, 40)
	for y := range s.Rows() {
		s.frame.Set(0, y, renderer.Cell{Rune: 'x', Style: renderer.DefaultStyle()})
		s.buffer.boundaries[y] = LineBound{End: 1}
	}
	b.ReportAllocs()
	for b.Loop() {
		recoveryTranscriptSnapshotSink = s.RecoveryTranscriptSnapshot()
	}
}
