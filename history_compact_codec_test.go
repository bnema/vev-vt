package vt

import (
	"bytes"
	"encoding/binary"
	"testing"

	renderer "github.com/bnema/vev-vt/core"
	"github.com/stretchr/testify/require"
)

func TestCompactHistoryDictionaryAndAccounting(t *testing.T) {
	h := NewHistory(HistoryConfig{MaxRows: 4, MaxBytes: 1 << 20, ChunkRows: 2})
	style := renderer.DefaultStyle()
	style.Bold = true
	style.HasForegroundRGB = true
	style.ForegroundRGB = renderer.RGB{R: 1, G: 2, B: 3}
	row := []renderer.Cell{{Rune: 'a', Style: style}, {Rune: 'b', Style: style}}
	for range 2 {
		require.NoError(t, h.Append(row, LineBound{End: 2}))
	}
	view := h.SealAndView()
	blob, err := MarshalHistory(view)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(blob, []byte("VTC1")))
	require.Len(t, blob, historyHeaderBytes+historyChunkHeaderBytes+historyStyleBytes+2*historyRowBytes+4*historyStoredCellBytes)
	stats, err := PreflightHistoryBlob(blob)
	require.NoError(t, err)
	require.Equal(t, uint64(2), stats.Styles)
	require.Equal(t, view.LogicalBytes(), stats.Bytes)
	decoded, err := UnmarshalHistory(blob)
	require.NoError(t, err)
	require.Equal(t, view.LogicalBytes(), decoded.LogicalBytes())
	require.NoError(t, decoded.Chunk(0).CheckInvariants())
	again, err := MarshalHistory(decoded)
	require.NoError(t, err)
	require.Equal(t, blob, again)
}

func TestCompactHistoryRejectsMalformedReferences(t *testing.T) {
	plain := historyPayloadWithDimensions(1, 2)
	const rowOffset = historyHeaderBytes + historyChunkHeaderBytes
	const cellOffset = rowOffset + historyRowBytes
	for _, tt := range []struct {
		name   string
		mutate func([]byte)
	}{
		{"style reference", func(b []byte) { binary.BigEndian.PutUint32(b[cellOffset+4:], 1) }},
		{"continuation flags", func(b []byte) { b[cellOffset+8] = 2 }},
		{"invalid rune", func(b []byte) { binary.BigEndian.PutUint32(b[cellOffset:], 0xd800) }},
		{"zero style count", func(b []byte) { clear(b[24:28]) }},
		{"excessive style count", func(b []byte) { binary.BigEndian.PutUint32(b[24:], ^uint32(0)) }},
		{"excessive width", func(b []byte) { binary.BigEndian.PutUint32(b[16:], ^uint32(0)) }},
		{"bound extent", func(b []byte) { binary.BigEndian.PutUint32(b[rowOffset+8:], 3) }},
		{"bound flag", func(b []byte) { b[rowOffset+12] = 2 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b := bytes.Clone(plain)
			tt.mutate(b)
			_, err := PreflightHistoryBlob(b)
			require.Error(t, err)
			_, err = UnmarshalHistory(b)
			require.Error(t, err)
		})
	}
}

func TestCompactHistoryRejectsMalformedDictionaries(t *testing.T) {
	style := renderer.DefaultStyle()
	style.Bold = true
	other := style
	other.Italic = true
	makeBlob := func(styles []renderer.Style, refs ...uint32) []byte {
		b := binary.BigEndian.AppendUint32([]byte(historyMagic), 1)
		b = binary.BigEndian.AppendUint64(b, 2)
		b = binary.BigEndian.AppendUint32(b, uint32(len(refs)))
		b = binary.BigEndian.AppendUint32(b, 1)
		b = binary.BigEndian.AppendUint32(b, uint32(len(styles)+1))
		for _, s := range styles {
			b = appendHistoryStyle(b, s)
		}
		b = binary.BigEndian.AppendUint64(b, 1)
		b = appendHistoryBound(b, LineBound{End: len(refs)})
		for _, ref := range refs {
			b = binary.BigEndian.AppendUint32(b, 'x')
			b = binary.BigEndian.AppendUint32(b, ref)
			b = append(b, 0)
		}
		return b
	}
	valid := makeBlob([]renderer.Style{style, other}, 1, 2)
	_, err := UnmarshalHistory(valid)
	require.NoError(t, err)
	noncanonical := bytes.Clone(valid)
	// Unused underline color is required to be canonical zero.
	noncanonical[historyHeaderBytes+historyChunkHeaderBytes+33] = 7
	badFlags := bytes.Clone(valid)
	badFlags[historyHeaderBytes+historyChunkHeaderBytes] |= 0x80
	for _, tt := range []struct {
		name string
		data []byte
	}{
		{"duplicate default", makeBlob([]renderer.Style{renderer.DefaultStyle()}, 1)},
		{"duplicate style", makeBlob([]renderer.Style{style, style}, 1, 2)},
		{"unused style", makeBlob([]renderer.Style{style, other}, 1, 1)},
		{"inactive fields", noncanonical},
		{"reserved flags", badFlags},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := PreflightHistoryBlob(tt.data)
			require.Error(t, err)
			_, err = UnmarshalHistory(tt.data)
			require.Error(t, err)
		})
	}
}

func BenchmarkCompactHistoryCodec(b *testing.B) {
	view := historyViewWithDimensions(10_000, 120)
	blob, err := MarshalHistory(view)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("encode", func(b *testing.B) {
		b.ReportAllocs()
		b.ReportMetric(float64(len(blob)), "encoded-B")
		for b.Loop() {
			if _, err := MarshalHistory(view); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("decode", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(blob)))
		for b.Loop() {
			if _, err := UnmarshalHistory(blob); err != nil {
				b.Fatal(err)
			}
		}
	})
}
