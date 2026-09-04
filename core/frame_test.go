package core

import "testing"

func TestFrameCanonicalOffsetsAndInvariants(t *testing.T) {
	f := NewFrame(4, 3)
	for y := range f.Height {
		if f.lineOffset[y] != y*f.Width {
			t.Fatalf("lineOffset[%d] = %d, want %d", y, f.lineOffset[y], y*f.Width)
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

func TestCheckInvariantsDetectsBrokenRotation(t *testing.T) {
	f := NewFrame(4, 3)
	f.lineOffset[1] = f.lineOffset[0]
	if err := f.CheckInvariants(); err == nil {
		t.Fatal("expected duplicate physical row to be rejected")
	}

	f = NewFrame(4, 3)
	f.lineOffset[0] = 1
	if err := f.CheckInvariants(); err == nil {
		t.Fatal("expected non-multiple offset to be rejected")
	}

	f = NewFrame(4, 3)
	f.lineOffset[2] = 99
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
