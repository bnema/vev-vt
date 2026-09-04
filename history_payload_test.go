package vt

import (
	"bytes"
	"encoding/binary"
	"testing"

	renderer "github.com/bnema/vev-vt/core"
	"github.com/stretchr/testify/require"
)

func TestPayloadSurvivesSnapshotResizeAndRecovery(t *testing.T) {
	payload, err := renderer.NewCellPayload("e\u0301", "https://example.com")
	require.NoError(t, err)
	s := NewScreen(4, 2)
	s.Write([]byte("ex"))
	cell := s.Cell(0, 0)
	cell.Payload = payload
	s.frame.Set(0, 0, cell)
	snapshot := s.Snapshot()
	recovery := s.RecoveryTranscriptSnapshot()
	s.Resize(2, 3)
	require.Equal(t, payload, s.Cell(0, 0).Payload)
	s.Write([]byte("\x1b[2J"))
	require.Equal(t, payload, snapshot.Cell(0, 0).Payload)
	blob, err := recovery.Marshal()
	require.NoError(t, err)
	view, err := UnmarshalHistory(blob)
	require.NoError(t, err)
	require.Equal(t, payload, view.Cell(0, 0).Payload)
}

func TestHistoryPayloadRoundTripEvictionAndRestore(t *testing.T) {
	payload, err := renderer.NewCellPayload("e\u0301", "https://example.com")
	require.NoError(t, err)
	h := NewHistory(HistoryConfig{MaxRows: 2, MaxBytes: 1 << 20, ChunkRows: 2})
	row := []renderer.Cell{{Rune: 'e', Style: renderer.DefaultStyle(), Payload: payload}}
	require.NoError(t, h.Append(row, LineBound{End: 1}))
	require.NoError(t, h.Append(historyRow("x"), LineBound{End: 1}))
	before := h.View()
	encoded, err := MarshalHistory(before)
	require.NoError(t, err)
	decoded, err := UnmarshalHistory(encoded)
	require.NoError(t, err)
	require.Equal(t, payload, decoded.Cell(0, 0).Payload)
	require.Equal(t, before.LogicalBytes(), decoded.LogicalBytes())
	require.NoError(t, decoded.Chunk(0).CheckInvariants())
	require.NoError(t, h.Append(historyRow("y"), LineBound{End: 1}))
	require.Equal(t, uint64(0), h.View().Chunk(0).payloadBytes)
	require.Equal(t, payload, before.Cell(0, 0).Payload, "eviction must not mutate a sealed borrowed view")
	sealed, tail, err := MarshalSealedHistory(before)
	require.NoError(t, err)
	restored, err := HistoryFromBlobs(HistoryConfig{MaxRows: 1, MaxBytes: 1 << 20}, sealed, tail)
	require.NoError(t, err)
	require.Equal(t, 'x', restored.View().Cell(0, 0).Rune)
	require.True(t, restored.View().Cell(0, 0).Payload.Empty())
	require.NoError(t, restored.View().Chunk(0).CheckInvariants())
}

func TestHistoryPayloadBudgetRejectsBeforeMutation(t *testing.T) {
	payload, err := renderer.NewCellPayload("e\u0301", "https://example.com")
	require.NoError(t, err)
	rowBytes := renderer.StoredCellLogicalBytes + renderer.RowDescriptorLogicalBytes + renderer.StyleRecordLogicalBytes
	h := NewHistory(HistoryConfig{MaxRows: 2, MaxBytes: rowBytes + payload.LogicalBytes() - 1})
	err = h.Append([]renderer.Cell{{Rune: 'e', Style: renderer.DefaultStyle(), Payload: payload}}, LineBound{End: 1})
	require.ErrorIs(t, err, ErrHistoryRowTooLarge)
	require.Zero(t, h.Len())
	require.Zero(t, h.LogicalBytes())
}

func TestPayloadCodecRejectsInvalidDictionariesAndReferences(t *testing.T) {
	a, err := renderer.NewCellPayload("", "https://a")
	require.NoError(t, err)
	b, err := renderer.NewCellPayload("", "https://b")
	require.NoError(t, err)
	h := NewHistory(HistoryConfig{MaxRows: 1, MaxBytes: 1 << 20})
	require.NoError(t, h.Append([]renderer.Cell{
		{Rune: 'a', Style: renderer.DefaultStyle(), Payload: a},
		{Rune: 'b', Style: renderer.DefaultStyle(), Payload: b},
	}, LineBound{End: 2}))
	valid, err := MarshalHistory(h.View())
	require.NoError(t, err)
	const dict = historyHeaderBytes + historyChunkHeaderBytes
	firstLen := 8 + len(a.Hyperlink())
	for _, tt := range []struct {
		name   string
		mutate func([]byte)
	}{
		{"missing payload reference", func(v []byte) { binary.BigEndian.PutUint32(v[len(v)-4:], 3) }},
		{"zero explicit payload reference", func(v []byte) { clear(v[len(v)-4:]) }},
		{"unused payload", func(v []byte) { binary.BigEndian.PutUint32(v[len(v)-4:], 1) }},
		{"duplicate payload", func(v []byte) { copy(v[dict+firstLen:dict+2*firstLen], v[dict:dict+firstLen]) }},
		{"control injection", func(v []byte) { v[dict+8] = 0x1b }},
		{"invalid UTF8", func(v []byte) { v[dict+8] = 0xff }},
		{"oversized string", func(v []byte) { binary.BigEndian.PutUint32(v[dict+4:], renderer.MaxHyperlinkBytes+1) }},
		{"excessive dictionary", func(v []byte) { binary.BigEndian.PutUint32(v[28:], ^uint32(0)) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v := bytes.Clone(valid)
			tt.mutate(v)
			_, err := PreflightHistoryBlob(v)
			require.Error(t, err)
			_, err = UnmarshalHistory(v)
			require.Error(t, err)
		})
	}
	for end := range len(valid) {
		_, err := UnmarshalHistory(valid[:end])
		require.Error(t, err)
	}
}
