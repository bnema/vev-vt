package vt

import (
	"testing"

	renderer "github.com/bnema/vev-vt/core"
	"github.com/stretchr/testify/require"
)

func TestHistoryConfigurationUsesOnlyApplicationLimits(t *testing.T) {
	for _, tt := range []struct {
		name   string
		config HistoryConfig
		valid  bool
	}{
		{"disabled", HistoryConfig{}, true},
		{"both explicit", HistoryConfig{MaxRows: 20, MaxBytes: 4096}, true},
		{"bytes only", HistoryConfig{MaxBytes: 1024}, true},
		{"rows only", HistoryConfig{MaxRows: 4}, true},
		{"negative rows", HistoryConfig{MaxRows: -1}, false},
		{"negative grouping", HistoryConfig{ChunkRows: -1}, false},
		{"oversized grouping", HistoryConfig{ChunkRows: 257}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.valid {
				require.NoError(t, tt.config.Validate())
				return
			}
			require.ErrorIs(t, tt.config.Validate(), ErrInvalidHistoryConfig)
			require.Panics(t, func() { NewHistory(tt.config) })
			_, err := HistoryFromBlobs(tt.config, nil, nil)
			require.ErrorIs(t, err, ErrInvalidHistoryConfig)
		})
	}
	require.Zero(t, NewHistory(HistoryConfig{MaxRows: 4}).ByteCap(), "library must not supply a byte policy")
	disabled := NewHistory(HistoryConfig{})
	require.NoError(t, disabled.Append(historyRow("ignored"), LineBound{End: 7}))
	require.Zero(t, disabled.Len())
}

func TestHistoryByteOnlyLimit(t *testing.T) {
	const budget = 2*(renderer.StoredCellLogicalBytes+renderer.RowDescriptorLogicalBytes) + renderer.StyleRecordLogicalBytes
	h := NewHistory(HistoryConfig{MaxBytes: budget})
	for _, text := range []string{"a", "b", "c"} {
		require.NoError(t, h.Append(historyRow(text), LineBound{End: 1}))
	}
	require.Zero(t, h.Cap(), "no independent line ceiling")
	require.Equal(t, budget, h.ByteCap())
	require.Equal(t, []string{"b", "c"}, historyViewTexts(h.View()))
	require.Equal(t, budget, h.LogicalBytes())
	require.Zero(t, h.Limits().MaxRows)
}

func TestHistoryLimitsReloadIsValidatedAndPreservesViews(t *testing.T) {
	h := NewHistory(HistoryConfig{MaxRows: 8, MaxBytes: 1 << 20, ChunkRows: 8})
	for _, text := range []string{"a", "b", "c", "d"} {
		require.NoError(t, h.Append(historyRow(text), LineBound{End: 1}))
	}
	view := h.View()
	before := h.Limits()
	require.ErrorIs(t, h.SetLimits(HistoryConfig{MaxRows: -1}), ErrInvalidHistoryConfig)
	require.Equal(t, before, h.Limits())
	require.Equal(t, 4, h.Len())
	require.NoError(t, h.SetLimits(HistoryConfig{MaxRows: 2, MaxBytes: 1 << 20, ChunkRows: 1}))
	require.Equal(t, []string{"c", "d"}, historyViewTexts(h.View()))
	require.Equal(t, []string{"a", "b", "c", "d"}, historyViewTexts(view))
	next := h.NextRowID()
	require.NoError(t, h.SetLimits(HistoryConfig{}))
	require.Zero(t, h.Len())
	require.Zero(t, h.LogicalBytes())
	require.Equal(t, next, h.NextRowID())
	require.NoError(t, h.SetLimits(HistoryConfig{MaxBytes: 4096}))
	require.NoError(t, h.Append(historyRow("new"), LineBound{End: 3}))
	require.Equal(t, next, h.View().RowID(0))
}
