package vt

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	renderer "github.com/bnema/vev-vt/core"
	"github.com/stretchr/testify/require"
)

func compressionFixture(t testing.TB) *History {
	t.Helper()
	h := NewHistory(HistoryConfig{MaxRows: 64, MaxBytes: 1 << 20, ChunkRows: 16})
	row := historyRow(string(bytes.Repeat([]byte("a"), 80)))
	payload, err := renderer.NewCellPayload("e\u0301", "https://example.com")
	require.NoError(t, err)
	row[0].Payload = payload
	for range 48 {
		require.NoError(t, h.Append(row, LineBound{End: 80, Soft: true}))
	}
	return h
}

func compressFixture(t testing.TB, h *History) {
	t.Helper()
	n, err := h.CompressIdle(100)
	require.NoError(t, err)
	require.Zero(t, n, "first observation must not compress a recently read page")
	n, err = h.CompressIdle(100)
	require.NoError(t, err)
	require.Equal(t, 2, n)
}

func TestColdHistoryPreservesIdentityAccountingAndContents(t *testing.T) {
	h := compressionFixture(t)
	view := h.View()
	chunk := view.Chunk(0)
	logical := h.LogicalBytes()
	encoded, err := MarshalHistory(view)
	require.NoError(t, err)
	compressFixture(t, h)
	for _, sealed := range h.chunks {
		require.Equal(t, len(sealed.page.compressed), cap(sealed.page.compressed), "compressed backing must not retain buffer slack")
	}
	stats := h.CompressionStats()
	require.Equal(t, 2, stats.ColdPages)
	require.Positive(t, stats.CompressedBytes)
	require.Less(t, stats.CompressedBytes+stats.ResidentLogicalBytes, logical)
	require.Equal(t, logical, h.LogicalBytes())
	require.Same(t, chunk, h.View().Chunk(0))
	// Persistence and streaming search do not fill random-access caches.
	again, err := MarshalHistory(view)
	require.NoError(t, err)
	require.Equal(t, encoded, again)
	rows := 0
	require.NoError(t, view.Range(func(row []renderer.Cell) bool {
		require.Equal(t, 'a', row[0].Rune)
		require.Equal(t, "e\u0301", row[0].Payload.Grapheme())
		rows++
		return true
	}))
	require.Equal(t, 48, rows)
	require.Equal(t, 2, h.CompressionStats().ColdPages)
	require.Equal(t, 'a', view.Cell(0, 0).Rune)
	require.Equal(t, 1, h.CompressionStats().ColdPages)
	require.Equal(t, RowID(1), view.RowID(0))
	require.Equal(t, LineBound{End: 80, Soft: true}, view.Bound(0))
	require.NoError(t, chunk.CheckInvariants())
	_, err = h.CompressIdle(100)
	require.NoError(t, err)
	n, err := h.CompressIdle(100)
	require.NoError(t, err)
	require.Equal(t, 1, n, "an unread restored cache is released without recompression")
}

func TestColdHistoryIncrementalBudgetAndEviction(t *testing.T) {
	h := compressionFixture(t)
	before := h.View()
	for range 4 {
		n, err := h.CompressIdle(1)
		require.NoError(t, err)
		require.LessOrEqual(t, n, 1)
	}
	require.Equal(t, 2, h.CompressionStats().ColdPages)
	for range 20 {
		require.NoError(t, h.Append(historyRow("new"), LineBound{End: 3}))
	}
	require.Equal(t, 64, h.Len())
	require.Equal(t, RowID(5), h.View().RowID(0))
	require.Equal(t, RowID(1), before.RowID(0))
	require.Equal(t, 'a', before.Cell(0, 0).Rune)
	require.NoError(t, h.View().Chunk(0).CheckInvariants())
}

func TestColdHistoryRejectsCorruptedBacking(t *testing.T) {
	h := compressionFixture(t)
	compressFixture(t, h)
	chunk := h.View().Chunk(0)
	chunk.page.compressed[len(chunk.page.compressed)-1] ^= 0xff
	require.ErrorIs(t, chunk.Restore(), ErrHistoryCorrupt)
	_, err := MarshalHistory(h.View())
	require.ErrorIs(t, err, ErrHistoryCorrupt)
	func() {
		defer func() {
			value := recover()
			err, ok := value.(error)
			require.True(t, ok)
			require.True(t, errors.Is(err, ErrHistoryCorrupt))
		}()
		_ = h.View().Cell(0, 0)
		t.Fatal("corrupt read silently succeeded")
	}()
}

func TestColdHistoryRejectsBackingLengthCorruption(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*sealedPage)
	}{
		{"trailing data", func(p *sealedPage) { p.compressed = append(p.compressed, 0) }},
		{"truncated data", func(p *sealedPage) { p.compressed = p.compressed[:len(p.compressed)-1] }},
		{"short decoded length", func(p *sealedPage) { p.encodedSize-- }},
		{"oversized decoded length", func(p *sealedPage) { p.encodedSize = 257 << 20 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := compressionFixture(t)
			compressFixture(t, h)
			chunk := h.View().Chunk(0)
			tt.mutate(chunk.page)
			require.ErrorIs(t, chunk.Restore(), ErrHistoryCorrupt)
		})
	}
}

func TestColdHistoryBorrowedReadConcurrentWithMaintenance(t *testing.T) {
	h := compressionFixture(t)
	view := h.View()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 100 {
			if view.Cell(0, 0).Rune != 'a' {
				t.Error("borrowed contents changed")
				return
			}
		}
	}()
	for range 100 {
		_, err := h.CompressIdle(2)
		require.NoError(t, err)
	}
	wg.Wait()
}

func BenchmarkColdHistoryRestore(b *testing.B) {
	h := compressionFixture(b)
	compressFixture(b, h)
	chunk := h.View().Chunk(0)
	b.ReportAllocs()
	for b.Loop() {
		if err := chunk.Restore(); err != nil {
			b.Fatal(err)
		}
		if _, err := h.CompressIdle(100); err != nil {
			b.Fatal(err)
		}
		if _, err := h.CompressIdle(100); err != nil {
			b.Fatal(err)
		}
	}
}
