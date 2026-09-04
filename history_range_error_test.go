package vt

import (
	"testing"

	"github.com/bnema/vev-vt/core"
	"github.com/stretchr/testify/require"
)

func TestHistoryRangeReportsCorruptionWithoutPanicking(t *testing.T) {
	h := compressionFixture(t)
	compressFixture(t, h)
	view := h.View()
	page := view.Chunk(1).page
	page.compressed[len(page.compressed)-1] ^= 0xff
	calls := 0
	err := view.Range(func([]core.Cell) bool { calls++; return true })
	require.ErrorIs(t, err, ErrHistoryCorrupt)
	require.Equal(t, 16, calls, "iteration stops before yielding any row from the corrupt page")
	calls = 0
	require.NoError(t, view.Range(func([]core.Cell) bool { calls++; return false }))
	require.Equal(t, 1, calls, "early cancellation must not restore later pages")
	require.NoError(t, (HistoryView{}).Range(nil))
}
