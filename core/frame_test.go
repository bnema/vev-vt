package core

import (
	"reflect"
	"testing"
)

func TestFrameCanonicalOffsetsAndInvariants(t *testing.T) {
	f := NewFrame(4, 3)
	for y := range f.Height {
		if f.page.rows[y] != uint32(y*f.Width) {
			t.Fatalf("row offset %d = %d, want %d", y, f.page.rows[y], y*f.Width)
		}
	}
	if err := f.CheckInvariants(); err != nil {
		t.Fatalf("fresh frame invariants: %v", err)
	}
}

func TestFrameRowReturnsOwnedCells(t *testing.T) {
	f := NewFrame(2, 1)
	f.Set(0, 0, Cell{Rune: 'A'})

	row := f.Row(0)
	row[0] = BlankCell()

	if got := f.Cell(0, 0).Rune; got != 'A' {
		t.Fatalf("mutating Row result changed frame cell to %q", got)
	}
}

func TestFrameRowMutationOperationsUseLogicalRows(t *testing.T) {
	f := NewFrame(5, 2)
	f.WriteRow(0, 0, []Cell{{Rune: 'A'}, {Rune: 'B'}, {Rune: 'C'}, {Rune: 'D'}, {Rune: 'E'}})
	f.ScrollDown(0, 1, 1)

	f.CopyRow(1, 2, 0, 3)
	f.FillRow(1, 0, 2, BlankCell())

	got := f.Row(1)
	want := []rune{' ', ' ', 'A', 'B', 'C'}
	for x, r := range want {
		if got[x].Rune != r {
			t.Fatalf("cell %d = %q, want %q", x, got[x].Rune, r)
		}
	}
}

func TestStoredCellFitsCompactTarget(t *testing.T) {
	if got := reflect.TypeOf(storedCell{}).Size(); got < 8 || got > 16 {
		t.Fatalf("storedCell size = %d bytes, want 8..16", got)
	}
}

func TestFrameInternsCanonicalStylesLocally(t *testing.T) {
	f := NewFrame(3, 1)
	first := Style{Bold: true, Foreground: 2, ForegroundRGB: RGB{R: 9, G: 8, B: 7}}
	second := first
	second.ForegroundRGB = RGB{R: 1, G: 2, B: 3}

	f.Set(0, 0, Cell{Rune: 'a', Style: first})
	f.Set(1, 0, Cell{Rune: 'b', Style: second})
	f.Set(2, 0, Cell{Rune: 'c', Style: Style{}})

	if f.page.cells[0].styleID != f.page.cells[1].styleID {
		t.Fatal("semantically equal styles received different page-local IDs")
	}
	if f.page.cells[2].styleID == 0 {
		t.Fatal("zero Style collapsed into default style ID zero")
	}
	if got := f.StyleCount(); got != 3 {
		t.Fatalf("StyleCount() = %d, want default plus two semantic styles", got)
	}
	if err := f.CheckInvariants(); err != nil {
		t.Fatalf("frame invariants: %v", err)
	}
}

func TestFrameStyleChurnReusesUnreferencedSlots(t *testing.T) {
	f := NewFrame(1, 1)
	for i := range 1000 {
		f.Set(0, 0, Cell{Rune: 'x', Style: Style{Foreground: i}})
	}
	if got := f.StyleCount(); got != 2 {
		t.Fatalf("StyleCount() after churn = %d, want default plus live style", got)
	}
	if got := len(f.page.styles); got > 3 {
		t.Fatalf("style slots after churn = %d, want at most 3", got)
	}
	f.Set(0, 0, BlankCell())
	if got := f.StyleCount(); got != 1 {
		t.Fatalf("StyleCount() after clear = %d, want default only", got)
	}
	if err := f.CheckInvariants(); err != nil {
		t.Fatalf("frame invariants: %v", err)
	}
}

func TestFrameCrossPageWriteRemapsStyles(t *testing.T) {
	src := NewFrame(2, 1)
	src.Set(0, 0, Cell{Rune: 'a', Style: Style{Bold: true, Foreground: 4}})
	src.Set(1, 0, Cell{Rune: 'b', Style: Style{Italic: true, Background: 7}})
	dst := NewFrame(2, 1)
	dst.Set(0, 0, Cell{Rune: 'x', Style: Style{Foreground: 99}})

	dst.WriteRow(0, 0, src.Row(0))

	for x := range 2 {
		if !dst.Cell(x, 0).Equal(src.Cell(x, 0)) {
			t.Fatalf("cell %d changed crossing pages: got %+v want %+v", x, dst.Cell(x, 0), src.Cell(x, 0))
		}
	}
	if err := dst.CheckInvariants(); err != nil {
		t.Fatalf("destination invariants: %v", err)
	}
}

func TestFrameLogicalBytesAreDeterministic(t *testing.T) {
	f := NewFrame(3, 2)
	want := uint64(6*storedCellLogicalBytes + 2*rowDescriptorLogicalBytes + styleRecordLogicalBytes)
	if got := f.LogicalBytes(); got != want {
		t.Fatalf("LogicalBytes() = %d, want %d", got, want)
	}
	f.Set(0, 0, Cell{Rune: 'x', Style: Style{Bold: true}})
	if got := f.LogicalBytes(); got != want+styleRecordLogicalBytes {
		t.Fatalf("LogicalBytes() with one interned style = %d, want %d", got, want+styleRecordLogicalBytes)
	}
}

func TestCheckInvariantsDetectsBrokenRotation(t *testing.T) {
	f := NewFrame(4, 3)
	f.page.rows[1] = f.page.rows[0]
	if err := f.CheckInvariants(); err == nil {
		t.Fatal("expected duplicate physical row to be rejected")
	}

	f = NewFrame(4, 3)
	f.page.rows[0] = 1
	if err := f.CheckInvariants(); err == nil {
		t.Fatal("expected non-multiple offset to be rejected")
	}

	f = NewFrame(4, 3)
	f.page.rows[2] = 99
	if err := f.CheckInvariants(); err == nil {
		t.Fatal("expected out-of-range offset to be rejected")
	}
}

func TestReplacePreservesSelfAliasedScrolledRows(t *testing.T) {
	f := NewFrame(3, 3)
	for y, row := range []string{"abc", "def", "ghi"} {
		for x, r := range row {
			f.Set(x, y, Cell{Rune: r})
		}
	}
	f.ScrollUp(0, 2, 1)
	want := [][]Cell{f.Row(0), f.Row(1), f.Row(2)}

	f.Replace(f)

	for y := range want {
		got := f.Row(y)
		for x := range want[y] {
			if !got[x].Equal(want[y][x]) {
				t.Fatalf("cell (%d,%d) = %+v, want %+v", x, y, got[x], want[y][x])
			}
		}
	}
	if err := f.CheckInvariants(); err != nil {
		t.Fatalf("replaced frame invariants: %v", err)
	}
}

func TestReplacePreservesLogicalRows(t *testing.T) {
	dst := NewFrame(3, 3)
	dst.Set(0, 0, Cell{Rune: 'x'})
	dst.ScrollUp(0, 2, 1)

	src := NewFrame(3, 3)
	for y, row := range []string{"abc", "def", "ghi"} {
		for x, r := range row {
			src.Set(x, y, Cell{Rune: r})
		}
	}
	src.ScrollUp(0, 2, 1)

	dst.Replace(src)
	for y, want := range []string{"def", "ghi", "   "} {
		for x, r := range want {
			if got := dst.At(x, y).Rune; got != r {
				t.Fatalf("row %d col %d = %q, want %q", y, x, got, r)
			}
		}
	}
	if err := dst.CheckInvariants(); err != nil {
		t.Fatalf("replaced frame invariants: %v", err)
	}
}
