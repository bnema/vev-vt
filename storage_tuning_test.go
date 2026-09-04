package vt

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/bnema/vev-vt/core"
)

func TestHistoryTailPreallocationFollowsGeometry(t *testing.T) {
	h := NewHistory(HistoryConfig{MaxRows: 4, MaxBytes: 1 << 20, ChunkRows: 4})
	if err := h.Append(historyRow("abc"), LineBound{End: 3}); err != nil {
		t.Fatal(err)
	}
	if got := cap(h.tailCells); got > 12 {
		t.Fatalf("tail preallocation = %d cells, want at most 4x3", got)
	}
}

func BenchmarkHistoryChunkSize(b *testing.B) {
	for _, rows := range []int{32, 64, 128, 256} {
		b.Run(fmt.Sprintf("rows-%d", rows), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				h := NewHistory(HistoryConfig{MaxRows: 10_000, MaxBytes: 1 << 30, ChunkRows: rows})
				row := make([]core.Cell, 120)
				for x := range row {
					row[x] = core.Cell{Rune: rune('a' + x%26), Style: core.DefaultStyle()}
				}
				for range 10_000 {
					if err := h.Append(row, LineBound{End: 120}); err != nil {
						b.Fatal(err)
					}
				}
				benchmarkStorageHistorySink = h
			}
		})
	}
}

func BenchmarkSmallHistoryRetained(b *testing.B) {
	var retained uint64
	for b.Loop() {
		benchmarkStorageHistorySink = nil
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		h := NewHistory(HistoryConfig{MaxRows: 4, MaxBytes: 1 << 20, ChunkRows: 4})
		if err := h.Append(historyRow("abc"), LineBound{End: 3}); err != nil {
			b.Fatal(err)
		}
		benchmarkStorageHistorySink = h
		runtime.GC()
		runtime.ReadMemStats(&after)
		if after.HeapAlloc >= before.HeapAlloc {
			retained += after.HeapAlloc - before.HeapAlloc
		}
	}
	b.ReportMetric(float64(retained)/float64(b.N), "retained-B/op")
}
