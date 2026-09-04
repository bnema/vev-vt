package vt

import (
	"testing"

	"github.com/bnema/vev-vt/core"
	"github.com/stretchr/testify/require"
)

func TestHistoryRetentionCanExceedSingleBlobLimits(t *testing.T) {
	// Retention policy is application-owned; codec ceilings apply to each blob.
	h := NewHistory(HistoryConfig{MaxRows: maxHistoryRows + 1, ChunkRows: 256})
	for range maxHistoryRows + 1 {
		require.NoError(t, h.Append([]core.Cell{{Rune: 'x', Style: core.DefaultStyle()}}, LineBound{End: 1}))
	}
	view := h.SealAndView()
	require.Equal(t, maxHistoryRows+1, view.Len())
	_, err := MarshalHistory(view)
	require.Error(t, err, "one aggregate blob is intentionally bounded")
	chunks, tail, err := MarshalSealedHistory(view)
	require.NoError(t, err, "per-chunk persistence must preserve larger retained history")
	restored, err := HistoryFromBlobs(h.Limits(), chunks, tail)
	require.NoError(t, err)
	require.Equal(t, h.Len(), restored.Len())
}
