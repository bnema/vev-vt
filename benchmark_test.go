package vt

import (
	"bytes"
	"encoding/base64"
	"testing"

	renderer "github.com/bnema/vev-vt/core"
)

var (
	benchmarkHistoryViewSink     HistoryView
	benchmarkHistorySnapshotSink HistorySnapshotView
	benchmarkDamageCaptureSink   DamageCapture
	benchmarkHistoryBlobSink     []byte
	benchmarkHistoryBlobsSink    [][]byte
)

// TestHistoryViewPartialTailAllocationBounded guards the capture property used
// by persistence: retained sealed chunks are shared, while only the small
// mutable tail is copied into a view.
func TestHistoryViewPartialTailAllocationBounded(t *testing.T) {
	const (
		width     = 120
		chunkRows = 64
		tailRows  = 7
	)
	sealed := benchmarkHistory(t, chunkRows*4, width, chunkRows)
	sealed.SealAndView()
	partial := benchmarkHistory(t, chunkRows*4+tailRows, width, chunkRows)
	partialBefore := partial.View()

	// Copying a partial tail costs one allocation per row plus a fixed set of
	// headers: the chunk slice, its growth for the tail chunk, the chunk itself,
	// the row slice, and the bounds slice parallel to it.
	const tailHeaders = 4

	sealedAllocs := testing.AllocsPerRun(20, func() { benchmarkHistoryViewSink = sealed.View() })
	partialAllocs := testing.AllocsPerRun(20, func() { benchmarkHistoryViewSink = partial.View() })
	if partialAllocs > sealedAllocs+tailRows+tailHeaders {
		t.Fatalf("partial-tail View allocations = %v, sealed=%v, tail rows=%d; sealed history was copied", partialAllocs, sealedAllocs, tailRows)
	}
	if partial.View().Chunk(0) != partialBefore.Chunk(0) {
		t.Fatal("unchanged sealed chunks must retain identity across partial-tail views")
	}
}

func BenchmarkHistoryView10KRowsSealedReuse(b *testing.B) {
	history := benchmarkHistory(b, 10_000, 120, 256)
	sealed := history.SealAndView()
	if sealed.Len() != 10_000 || sealed.ChunkCount() != 40 || sealed.Chunk(0) == nil {
		b.Fatalf("invalid sealed history fixture: rows=%d chunks=%d", sealed.Len(), sealed.ChunkCount())
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		benchmarkHistoryViewSink = history.View()
	}
	b.StopTimer()
	if got := benchmarkHistoryViewSink; got.Len() != sealed.Len() || got.Chunk(0) != sealed.Chunk(0) {
		b.Fatal("unchanged sealed history chunks were not reused")
	}
	b.ReportMetric(float64(sealed.ChunkCount()), "sealedchunks/view")
}

func BenchmarkHistoryView10KRowsPartialTail(b *testing.B) {
	history := benchmarkHistory(b, 10_000, 120, 256)
	before := history.View()
	if before.Len() != 10_000 || before.ChunkCount() != 40 || before.Chunk(0) == nil {
		b.Fatalf("invalid partial-tail history fixture: rows=%d chunks=%d", before.Len(), before.ChunkCount())
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		benchmarkHistoryViewSink = history.View()
	}
	b.StopTimer()
	if got := benchmarkHistoryViewSink; got.Len() != before.Len() || got.Chunk(0) != before.Chunk(0) {
		b.Fatal("partial-tail capture did not reuse its sealed chunks")
	}
	b.ReportMetric(float64(10_000%256), "tailrows/view")
}

func BenchmarkHistorySnapshotView10KRowsPartialTail(b *testing.B) {
	history := benchmarkHistory(b, 10_000, 120, 256)
	if got, want := len(history.tail), 16; got != want {
		b.Fatalf("partial tail rows = %d, want %d", got, want)
	}
	sealed := history.View().Chunk(0)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		benchmarkHistorySnapshotSink = history.SnapshotView()
	}
	b.StopTimer()
	if got := benchmarkHistorySnapshotSink; got.Chunk(0) != sealed || got.Tail().Len() != 16 {
		b.Fatal("snapshot did not preserve chunk identity and tail rows")
	}
	b.ReportMetric(float64(benchmarkHistorySnapshotSink.ChunkCount()), "sealedchunks/view")
}

func BenchmarkMarshalHistoryChunk256x120(b *testing.B) {
	history := benchmarkHistory(b, 256, 120, 256)
	chunk := history.SealAndView().Chunk(0)
	if chunk == nil {
		b.Fatal("invalid history chunk fixture")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var err error
		benchmarkHistoryBlobSink, err = MarshalHistoryChunk(chunk)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if _, err := PreflightHistoryBlob(benchmarkHistoryBlobSink); err != nil {
		b.Fatalf("encoded history chunk is invalid: %v", err)
	}
	b.SetBytes(int64(len(benchmarkHistoryBlobSink)))
}

func BenchmarkUnmarshalHistoryChunk256x120(b *testing.B) {
	history := benchmarkHistory(b, 256, 120, 256)
	blob, err := MarshalHistoryChunk(history.SealAndView().Chunk(0))
	if err != nil {
		b.Fatal(err)
	}
	if stats, err := PreflightHistoryBlob(blob); err != nil || stats.Rows != 256 || stats.Cells != 256*120 {
		b.Fatalf("invalid encoded history chunk fixture: stats=%+v err=%v", stats, err)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(blob)))
	b.ResetTimer()
	for b.Loop() {
		var err error
		benchmarkHistoryViewSink, err = UnmarshalHistory(blob)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if benchmarkHistoryViewSink.Len() != 256 || benchmarkHistoryViewSink.ChunkCount() != 1 {
		b.Fatal("decoded history chunk lost rows")
	}
}

func BenchmarkMarshalSealedHistory10KRows(b *testing.B) {
	history := benchmarkHistory(b, 10_000, 120, 256)
	view := history.SealAndView()
	if view.Len() != 10_000 || view.ChunkCount() != 40 {
		b.Fatalf("invalid sealed history fixture: rows=%d chunks=%d", view.Len(), view.ChunkCount())
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var err error
		benchmarkHistoryBlobsSink, benchmarkHistoryBlobSink, err = MarshalSealedHistory(view)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if len(benchmarkHistoryBlobsSink) != view.ChunkCount() || len(benchmarkHistoryBlobSink) == 0 {
		b.Fatal("sealed-history encoder produced an incomplete capture")
	}
	b.ReportMetric(float64(len(benchmarkHistoryBlobsSink)), "sealedchunks/op")
}

func benchmarkHistory(t testing.TB, rows, width, chunkRows int) *History {
	t.Helper()
	history := NewHistory(HistoryConfig{MaxRows: rows, ChunkRows: chunkRows})
	for row := range rows {
		cells := make([]renderer.Cell, width)
		for col := range cells {
			cells[col] = renderer.Cell{Rune: rune('a' + (row+col)%26)}
		}
		requireHistoryAppend(t, history, cells)
	}
	return history
}

// BenchmarkScreenResizeReflowViewport verifies resize only visits the live
// viewport. The 10K-history variant must track the control rather than history
// length; history is deliberately populated without changing the viewport.
func BenchmarkScreenResizeReflowViewport(b *testing.B) {
	b.Run("control", func(b *testing.B) { benchmarkScreenResizeReflow(b, 0) })
	b.Run("10k-history", func(b *testing.B) { benchmarkScreenResizeReflow(b, 10_000) })
}

func benchmarkScreenResizeReflow(b *testing.B, historyRows int) {
	b.Helper()
	s := NewScreenWithHistory(120, 40, HistoryConfig{MaxRows: historyRows + 500})
	if historyRows > 0 {
		for range historyRows {
			requireHistoryAppend(b, s.history, make([]renderer.Cell, 120))
		}
		// History normally receives rows evicted from this screen, whose IDs are
		// allocated after every existing live or historical row. This fixture
		// prepopulates history directly, so advance and refresh the live IDs to
		// preserve that invariant before exercising resize eviction.
		s.nextRowID = s.history.nextRowID - 1
		for y := range s.buffer.rowIDs {
			s.buffer.rowIDs[y] = s.nextRowIDValue()
		}
	}
	s.Write(bytes.Repeat([]byte("x"), 120*39))
	s.ClearDamage()
	sizes := [2][2]int{{80, 40}, {120, 40}}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		s.Resize(sizes[i%len(sizes)][0], sizes[i%len(sizes)][1])
		s.ClearDamage()
	}
	b.StopTimer()
	if got := s.History(); historyRows > 0 && got.Len() < historyRows {
		b.Fatalf("resize discarded retained history rows: got %d want at least %d", got.Len(), historyRows)
	}
}

func BenchmarkScreenPrintableASCII(b *testing.B) {
	s := NewScreen(120, 40)
	chunk := append(bytes.Repeat([]byte("x"), 119), '\r')
	b.SetBytes(int64(len(chunk)))
	b.ReportAllocs()
	for b.Loop() {
		s.Write(chunk)
		s.ClearDamage()
	}
}

func BenchmarkScreenKittyAPC(b *testing.B) {
	payload := base64.RawStdEncoding.EncodeToString(make([]byte, 256*256*4))
	apc := []byte("\x1b_Ga=t,i=1,f=32,s=256,v=256,q=2;" + payload + "\x1b\\")
	b.SetBytes(int64(len(apc)))
	for _, fragmented := range []bool{false, true} {
		b.Run(map[bool]string{false: "single-write", true: "byte-by-byte"}[fragmented], func(b *testing.B) {
			s := NewScreen(120, 40)
			b.ReportAllocs()
			for b.Loop() {
				if fragmented {
					for _, part := range apc {
						s.Write([]byte{part})
					}
				} else {
					s.Write(apc)
				}
				s.ClearDamage()
			}
		})
	}
}

func BenchmarkScreenMixedUTF8(b *testing.B) {
	s := NewScreen(120, 40)
	chunk := append(bytes.Repeat([]byte("Aé界"), 20), '\r')
	b.SetBytes(int64(len(chunk)))
	b.ReportAllocs()
	for b.Loop() {
		s.Write(chunk)
		s.ClearDamage()
	}
}

func BenchmarkScreenCSIHeavy(b *testing.B) {
	s := NewScreen(120, 40)
	chunk := []byte("\x1b[1;1H\x1b[2K\x1b[31;48;2;1;2;3mstatus\x1b[0m\x1b[10C\x1b[?25l\x1b[?25h\r")
	b.SetBytes(int64(len(chunk)))
	b.ReportAllocs()
	for b.Loop() {
		s.Write(chunk)
		s.ClearDamage()
	}
}

func BenchmarkScreenCaptureDamage(b *testing.B) {
	s := NewScreen(120, 40)
	chunk := []byte("x")
	s.ClearDamage()
	b.ReportAllocs()
	for b.Loop() {
		s.Write(chunk)
		benchmarkDamageCaptureSink = s.CaptureDamage()
		if len(benchmarkDamageCaptureSink.Damage) == 0 {
			b.Fatal("expected captured damage")
		}
		if !s.AcknowledgeDamage(benchmarkDamageCaptureSink.Generation) {
			b.Fatal("expected current damage acknowledgement")
		}
	}
}

func BenchmarkScreenShellRedrawBurst(b *testing.B) {
	chunk := []byte("\r\x1b[K❯ abc\x1b[90m autosuggestion\x1b[39m\r\x1b[5C")
	b.ReportAllocs()
	for b.Loop() {
		s := NewScreen(120, 40)
		for range 200 {
			s.Write(chunk)
			s.ClearDamage()
		}
	}
}

func BenchmarkScreenFullscreenScrollRegion(b *testing.B) {
	chunk := []byte("\x1b[2;39r\x1b[39;1Hline\n")
	b.ReportAllocs()
	for b.Loop() {
		s := NewScreen(120, 40)
		for range 500 {
			s.Write(chunk)
			s.ClearDamage()
		}
	}
}
