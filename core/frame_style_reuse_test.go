package core

import (
	"reflect"
	"testing"
)

func TestSetStyleReuse(t *testing.T) {
	for _, rotated := range []bool{false, true} {
		f := NewFrame(3, 2)
		if rotated {
			f.ScrollUp(0, 1, 1)
		}
		styles := []Style{DefaultStyle(), {Bold: true}, {Italic: true}, {Bold: true}, DefaultStyle()}
		for _, style := range styles {
			for repeat := range 3 {
				for x := range f.Width {
					cell := Cell{Rune: rune('a' + repeat), Style: style}
					// Inactive RGB fields differ without changing canonical style.
					cell.Style.ForegroundRGB = RGB{R: uint8(repeat + 1)}
					index := f.offset(x, 0)
					oldID := f.page.cells[index].styleID
					sameStyle := f.page.styles[oldID].style == style.Canonical()
					f.Set(x, 0, cell)
					if got := f.page.cells[index].styleID; sameStyle && got != oldID {
						t.Fatalf("equivalent style changed ID from %d to %d", oldID, got)
					}
					if got := f.Cell(x, 0); !got.Equal(cell) {
						t.Fatalf("cell = %+v, want %+v", got, cell)
					}
					if err := f.CheckInvariants(); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
}

func BenchmarkFrameRewriteSameStyle(b *testing.B) {
	f := NewFrame(182, 53)
	row := make([]Cell, f.Width)
	for x := range row {
		row[x] = Cell{Rune: 'x', Style: Style{Bold: true}}
	}
	for y := range f.Height {
		f.WriteRow(y, 0, row)
	}
	b.ReportAllocs()
	for b.Loop() {
		for y := range f.Height {
			f.WriteRow(y, 0, row)
		}
	}
}

func TestStyleCacheSurvivesSlotFreeAndReuse(t *testing.T) {
	a := Style{Foreground: 1}
	b := Style{Foreground: 2, Bold: true}
	f := NewFrame(4, 2)
	check := func(step string) {
		t.Helper()
		if err := f.CheckInvariants(); err != nil {
			t.Fatalf("%s: %v", step, err)
		}
	}

	f.Set(0, 0, Cell{Rune: 'a', Style: a})
	check("set a")
	idA := f.page.cells[f.offset(0, 0)].styleID
	if !f.page.styleCacheOK || f.page.styleCacheID != idA {
		t.Fatalf("cache = (%v,%d), want live slot %d", f.page.styleCacheOK, f.page.styleCacheID, idA)
	}

	// Overwrite the only A cell so its slot is freed; the cache must not keep it.
	f.Set(0, 0, BlankCell())
	check("free a")
	if f.page.styleCacheOK && !f.page.styles[f.page.styleCacheID].used {
		t.Fatal("cache points at a freed style slot")
	}

	// Reuse the freed slot for B, then request A again: it must not alias B.
	f.Set(1, 0, Cell{Rune: 'b', Style: b})
	check("reuse slot for b")
	if got := f.page.cells[f.offset(1, 0)].styleID; got != idA {
		t.Fatalf("expected freed slot %d to be reused, got %d", idA, got)
	}
	f.Set(2, 0, Cell{Rune: 'a', Style: a})
	check("set a again")
	if got := f.Cell(2, 0).Style; got != a {
		t.Fatalf("style after reuse = %+v, want %+v", got, a)
	}
	if got := f.Cell(1, 0).Style; got != b {
		t.Fatalf("style b = %+v, want %+v", got, b)
	}
	if f.StyleCount() != 3 {
		t.Fatalf("StyleCount = %d, want 3", f.StyleCount())
	}

	// Repeating the cached raw style must bump references exactly once per cell.
	f.Set(3, 0, Cell{Rune: 'a', Style: a})
	f.Set(0, 1, Cell{Rune: 'a', Style: a})
	check("repeat a")
	f.Set(2, 0, BlankCell())
	f.Set(3, 0, BlankCell())
	f.Set(0, 1, BlankCell())
	check("release a")
	if f.StyleCount() != 2 {
		t.Fatalf("StyleCount = %d, want 2", f.StyleCount())
	}
}

func TestStyleCacheNonCanonicalInput(t *testing.T) {
	f := NewFrame(3, 1)
	rgbWithStaleIndex := Style{Foreground: 7, HasForegroundRGB: true, ForegroundRGB: RGB{R: 1}, Background: -1}
	canonical := rgbWithStaleIndex.Canonical()
	f.Set(0, 0, Cell{Rune: 'x', Style: rgbWithStaleIndex})
	f.Set(1, 0, Cell{Rune: 'y', Style: canonical})
	f.Set(2, 0, Cell{Rune: 'z', Style: rgbWithStaleIndex})
	if err := f.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
	if f.StyleCount() != 2 {
		t.Fatalf("StyleCount = %d, want 2", f.StyleCount())
	}
	for x := range 3 {
		if got := f.Cell(x, 0).Style; got != canonical {
			t.Fatalf("cell %d style = %+v, want %+v", x, got, canonical)
		}
	}
	// Rewriting with the non-canonical form must keep reference counts exact.
	f.Set(0, 0, Cell{Rune: 'x', Style: rgbWithStaleIndex})
	if err := f.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestStyleCacheCloneAndReplaceIndependence(t *testing.T) {
	a := Style{Foreground: 1}
	b := Style{Foreground: 2}
	src := NewFrame(2, 1)
	src.Set(0, 0, Cell{Rune: 'a', Style: a})
	clone := src.Clone()
	if !reflect.DeepEqual(src, clone) {
		t.Fatal("clone is not DeepEqual to its source")
	}

	// Free A in the source and reuse its slot for B; the clone keeps A.
	src.Set(0, 0, BlankCell())
	src.Set(1, 0, Cell{Rune: 'b', Style: b})
	clone.Set(1, 0, Cell{Rune: 'a', Style: a})
	for name, f := range map[string]Frame{"src": src, "clone": clone} {
		if err := f.CheckInvariants(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if got := clone.Cell(0, 0).Style; got != a {
		t.Fatalf("clone cell 0 = %+v, want %+v", got, a)
	}
	if got := clone.Cell(1, 0).Style; got != a {
		t.Fatalf("clone cell 1 = %+v, want %+v", got, a)
	}
	if got := src.Cell(1, 0).Style; got != b {
		t.Fatalf("src cell 1 = %+v, want %+v", got, b)
	}

	replaced := NewFrame(2, 1)
	replaced.Set(0, 0, Cell{Rune: 'q', Style: b})
	replaced.Replace(clone)
	replaced.Set(0, 0, Cell{Rune: 'q', Style: b})
	replaced.Set(1, 0, Cell{Rune: 'q', Style: b})
	if err := replaced.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
	if err := clone.CheckInvariants(); err != nil || clone.Cell(0, 0).Style != a {
		t.Fatalf("clone changed by replaced frame: %v", err)
	}
}

func TestBlankPhysicalRowReleasesStylesAndPayloads(t *testing.T) {
	const width = 7
	f := NewFrame(width, 3)
	payload, err := NewCellPayload("é", "")
	if err != nil {
		t.Fatal(err)
	}
	for x := range width {
		f.Set(x, 0, Cell{Rune: 'x', Style: Style{Foreground: x % 3}, Payload: payload})
		f.Set(x, 1, Cell{Rune: 'y', Style: Style{Foreground: x % 3}})
	}
	f.ScrollUp(0, 2, 1)
	if err := f.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
	for x := range width {
		if got := f.Cell(x, 2); got != (Cell{Rune: ' ', Style: DefaultStyle()}) {
			t.Fatalf("blank row cell %d = %+v", x, got)
		}
		if got := f.Cell(x, 0); got.Rune != 'y' || !got.Payload.Empty() {
			t.Fatalf("shifted row cell %d = %+v", x, got)
		}
	}
	f.ScrollDown(0, 2, 3)
	if err := f.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
	if f.StyleCount() != 1 {
		t.Fatalf("StyleCount = %d, want 1", f.StyleCount())
	}
}
