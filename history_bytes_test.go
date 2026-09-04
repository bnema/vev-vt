package vt

import (
	"errors"
	"testing"

	renderer "github.com/bnema/vev-vt/core"
)

func TestHistoryBoundsRetainedLogicalBytes(t *testing.T) {
	const budget = 2*(2*renderer.StoredCellLogicalBytes+renderer.RowDescriptorLogicalBytes) + renderer.StyleRecordLogicalBytes
	history := NewHistory(HistoryConfig{MaxRows: 8, MaxBytes: budget, ChunkRows: 4})
	for _, text := range []string{"aa", "bb", "cc"} {
		requireHistoryAppend(t, history, historyRow(text))
	}

	if got, want := historyViewTexts(history.View()), []string{"bb", "cc"}; !equalStrings(got, want) {
		t.Fatalf("retained rows = %#v, want %#v", got, want)
	}
	if got, want := history.LogicalBytes(), budget; got != want {
		t.Fatalf("logical bytes = %d, want %d", got, want)
	}
	if got, want := history.View().LogicalBytes(), budget; got != want {
		t.Fatalf("view logical bytes = %d, want %d", got, want)
	}
	if got, want := history.ByteCap(), budget; got != want {
		t.Fatalf("byte capacity = %d, want %d", got, want)
	}
}

func TestHistoryLogicalBytesDeduplicateCanonicalStylesPerSlab(t *testing.T) {
	const budget = 2*(renderer.StoredCellLogicalBytes+renderer.RowDescriptorLogicalBytes) + 2*renderer.StyleRecordLogicalBytes
	history := NewHistory(HistoryConfig{MaxRows: 8, MaxBytes: budget, ChunkRows: 2})
	style := renderer.Style{Bold: true}
	for _, r := range []rune{'a', 'b'} {
		if err := history.Append([]renderer.Cell{{Rune: r, Style: style}}, LineBound{End: 1}); err != nil {
			t.Fatalf("append %q: %v", r, err)
		}
	}

	if got, want := history.LogicalBytes(), budget; got != want {
		t.Fatalf("logical bytes = %d, want %d", got, want)
	}
	if got := history.SnapshotView().LogicalBytes(); got != history.LogicalBytes() {
		t.Fatalf("snapshot logical bytes = %d, want %d", got, history.LogicalBytes())
	}
	if got := history.SealAndView().LogicalBytes(); got != history.LogicalBytes() {
		t.Fatalf("sealed view logical bytes = %d, want %d", got, history.LogicalBytes())
	}
}

func TestHistoryLogicalBytesRemainStableAfterStyledRowEvictionAndCodecRoundTrip(t *testing.T) {
	history := NewHistory(HistoryConfig{MaxRows: 3, MaxBytes: 1 << 20, ChunkRows: 3})
	styles := []renderer.Style{{Bold: true}, renderer.DefaultStyle(), renderer.DefaultStyle(), renderer.DefaultStyle()}
	for i, style := range styles {
		if err := history.Append([]renderer.Cell{{Rune: rune('a' + i), Style: style}}, LineBound{End: 1}); err != nil {
			t.Fatalf("append row %d: %v", i, err)
		}
	}

	before := history.View()
	blob, err := MarshalHistory(before)
	if err != nil {
		t.Fatalf("marshal history: %v", err)
	}
	after, err := UnmarshalHistory(blob)
	if err != nil {
		t.Fatalf("unmarshal history: %v", err)
	}
	if got, want := after.LogicalBytes(), before.LogicalBytes(); got != want {
		t.Fatalf("round-trip logical bytes = %d, want %d", got, want)
	}
}

func TestHistoryRejectsSingleRowLargerThanLogicalByteBudget(t *testing.T) {
	history := NewHistory(HistoryConfig{MaxRows: 8, MaxBytes: 79, ChunkRows: 4})
	row := []renderer.Cell{{Rune: 'x', Style: renderer.Style{Bold: true}}}

	err := history.Append(row, LineBound{End: 1})
	if !errors.Is(err, ErrHistoryRowTooLarge) {
		t.Fatalf("append oversized row error = %v, want ErrHistoryRowTooLarge", err)
	}
	if history.Len() != 0 || history.LogicalBytes() != 0 {
		t.Fatalf("rejected append mutated history: rows=%d bytes=%d", history.Len(), history.LogicalBytes())
	}
}

func BenchmarkHistoryByteLimitedAppend(b *testing.B) {
	const rows, width = 256, 120
	maxBytes := uint64(rows)*(uint64(width)*renderer.StoredCellLogicalBytes+renderer.RowDescriptorLogicalBytes) + renderer.StyleRecordLogicalBytes
	history := NewHistory(HistoryConfig{MaxRows: rows * 2, MaxBytes: maxBytes, ChunkRows: rows})
	row := make([]renderer.Cell, width)
	for range rows {
		requireHistoryAppend(b, history, row)
	}

	b.ReportAllocs()
	b.SetBytes(int64(width) * int64(renderer.StoredCellLogicalBytes))
	b.ResetTimer()
	for b.Loop() {
		requireHistoryAppend(b, history, row)
	}
	b.StopTimer()
	if history.LogicalBytes() > maxBytes {
		b.Fatalf("logical bytes = %d, exceeds capacity %d", history.LogicalBytes(), maxBytes)
	}
}

func TestHistoryRestoreEvictsToLogicalByteBudget(t *testing.T) {
	source := NewHistory(HistoryConfig{MaxRows: 8, MaxBytes: 1 << 20, ChunkRows: 4})
	for _, text := range []string{"aa", "bb", "cc"} {
		requireHistoryAppend(t, source, historyRow(text))
	}
	sealed, tail, err := MarshalSealedHistory(source.SealAndView())
	if err != nil {
		t.Fatalf("marshal history: %v", err)
	}

	const budget = 2*(2*renderer.StoredCellLogicalBytes+renderer.RowDescriptorLogicalBytes) + renderer.StyleRecordLogicalBytes
	restored, err := HistoryFromBlobs(HistoryConfig{MaxRows: 8, MaxBytes: budget, ChunkRows: 4}, sealed, tail)
	if err != nil {
		t.Fatalf("restore history: %v", err)
	}
	if got, want := historyViewTexts(restored.View()), []string{"bb", "cc"}; !equalStrings(got, want) {
		t.Fatalf("restored rows = %#v, want %#v", got, want)
	}
	if got := restored.LogicalBytes(); got > restored.ByteCap() {
		t.Fatalf("restored logical bytes = %d, exceeds capacity %d", got, restored.ByteCap())
	}
}
