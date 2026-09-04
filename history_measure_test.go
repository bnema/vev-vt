package vt

import (
	"encoding/binary"
	"testing"

	"github.com/bnema/vev-vt/core"
	"github.com/stretchr/testify/require"
)

func TestMeasureHistoryBlobMatchesValidatedResources(t *testing.T) {
	payload, err := core.NewCellPayload("e\u0301", "https://example.org")
	require.NoError(t, err)
	for _, rows := range []int{0, 1, 8} {
		h := NewHistory(HistoryConfig{MaxRows: 10, ChunkRows: 2})
		for i := range rows {
			style := core.DefaultStyle()
			style.Foreground = i
			require.NoError(t, h.Append([]core.Cell{{Rune: 'e', Style: style, Payload: payload}}, LineBound{End: 1}))
		}
		data, err := MarshalHistory(h.SealAndView())
		require.NoError(t, err)
		want, err := PreflightHistoryBlob(data)
		require.NoError(t, err)
		got, err := MeasureHistoryBlob(data)
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Zero(t, testing.AllocsPerRun(20, func() {
			if _, err := MeasureHistoryBlob(data); err != nil {
				panic(err)
			}
		}))
		for n := range len(data) {
			_, err := MeasureHistoryBlob(data[:n])
			require.Error(t, err)
		}
		_, err = MeasureHistoryBlob(append(append([]byte(nil), data...), 0))
		require.Error(t, err)
	}
}

func TestMeasureHistoryBlobDoesNotReplaceValidation(t *testing.T) {
	h := NewHistory(HistoryConfig{MaxRows: 1})
	require.NoError(t, h.Append([]core.Cell{{Rune: 'a', Style: core.DefaultStyle()}}, LineBound{End: 1}))
	data, err := MarshalHistory(h.SealAndView())
	require.NoError(t, err)
	binary.BigEndian.PutUint64(data[historyHeaderBytes+historyChunkHeaderBytes:], 0)
	_, err = MeasureHistoryBlob(data)
	require.NoError(t, err, "resource sizing deliberately does not validate row identities")
	_, err = PreflightHistoryBlob(data)
	require.Error(t, err, "semantic validation must still reject the invalid identity")
}

func FuzzMeasureHistoryBlob(f *testing.F) {
	empty, err := MarshalEmptyHistoryTail()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(empty)
	f.Fuzz(func(t *testing.T, data []byte) {
		got, measured := MeasureHistoryBlob(data)
		want, validated := PreflightHistoryBlob(data)
		if validated == nil {
			require.NoError(t, measured)
			require.Equal(t, want, got)
		}
	})
}
