package core

import (
	"fmt"
	"reflect"
	"testing"
)

// cellPageFieldCount is the number of cellPage fields CopyFrom handles. Adding
// a field to cellPage fails TestCopyFromHandlesEveryPageField until CopyFrom
// (and this count) are updated.
const cellPageFieldCount = 13

func copyTestPayload(t *testing.T, grapheme, link string) CellPayload {
	t.Helper()
	p, err := NewCellPayload(grapheme, link)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func copyTestFrame(t *testing.T, width, height int, seed rune) Frame {
	t.Helper()
	f := NewFrame(width, height)
	styles := []Style{DefaultStyle(), {Bold: true, Foreground: 3, Background: -1}, {Italic: true, HasForegroundRGB: true, ForegroundRGB: RGB{R: 1, G: 2, B: 3}, Background: -1}}
	for y := range height {
		for x := range width {
			c := Cell{Rune: seed + rune((x+y)%7), Style: styles[(x+2*y)%len(styles)]}
			if (x+y)%5 == 0 {
				c.Payload = copyTestPayload(t, "e\u0301", "https://example.test/"+string(rune('a'+y%3)))
			}
			f.Set(x, y, c)
		}
	}
	if height > 2 {
		f.ScrollUp(0, height-1, 1)
	}
	return f
}

func requireFramesEqual(t *testing.T, got, want Frame) {
	t.Helper()
	if got.Width != want.Width || got.Height != want.Height {
		t.Fatalf("size %dx%d, want %dx%d", got.Width, got.Height, want.Width, want.Height)
	}
	for y := range want.Height {
		if !reflect.DeepEqual(got.Row(y), want.Row(y)) {
			t.Fatalf("row %d = %+v, want %+v", y, got.Row(y), want.Row(y))
		}
	}
	if err := got.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestCopyFromIsIndependentAndReusesStorage(t *testing.T) {
	src := copyTestFrame(t, 9, 5, 'a')
	var dst Frame
	dst.CopyFrom(src)
	requireFramesEqual(t, dst, src)
	if dst.page == src.page {
		t.Fatal("copy aliases source page")
	}

	// Mutating the source must not affect the copy.
	before := dst.Clone()
	src.Set(0, 0, Cell{Rune: 'Z', Style: Style{Bold: true, Foreground: 9, Background: -1}})
	requireFramesEqual(t, dst, before)

	// Copy over a different, larger-state destination: the page pointer and
	// backing arrays are reused and stale styles/payloads do not leak.
	other := copyTestFrame(t, 9, 5, 'k')
	other.Set(1, 1, Cell{Rune: 'q', Style: Style{Attrs: AttrUnderline, Foreground: -1, Background: -1}})
	page := dst.page
	cells := &dst.page.cells[0]
	dst.CopyFrom(other)
	if dst.page != page || &dst.page.cells[0] != cells {
		t.Fatal("CopyFrom did not reuse destination storage")
	}
	requireFramesEqual(t, dst, other)
	if dst.LogicalBytes() != other.LogicalBytes() || dst.StyleCount() != other.StyleCount() {
		t.Fatalf("accounting mismatch: %d/%d vs %d/%d", dst.LogicalBytes(), dst.StyleCount(), other.LogicalBytes(), other.StyleCount())
	}

	// The copy stays a fully functional, independent frame.
	dst.Set(2, 2, Cell{Rune: 'n', Style: DefaultStyle()})
	dst.ScrollDown(0, 4, 2)
	if err := dst.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
	requireFramesEqual(t, other, withCell(copyTestFrame(t, 9, 5, 'k'), 1, 1, Cell{Rune: 'q', Style: Style{Attrs: AttrUnderline, Foreground: -1, Background: -1}}))
}

func withCell(f Frame, x, y int, c Cell) Frame { f.Set(x, y, c); return f }

func TestCopyFromResizesAndIgnoresInvalidSource(t *testing.T) {
	dst := copyTestFrame(t, 4, 3, 'a')
	small := copyTestFrame(t, 2, 2, 'x')
	dst.CopyFrom(small)
	requireFramesEqual(t, dst, small)
	large := copyTestFrame(t, 12, 7, 'm')
	dst.CopyFrom(large)
	requireFramesEqual(t, dst, large)

	before := dst.Clone()
	dst.CopyFrom(Frame{})
	dst.CopyFrom(Frame{Width: 3, Height: 3})
	requireFramesEqual(t, dst, before)
	dst.CopyFrom(dst)
	requireFramesEqual(t, dst, before)
	var nilFrame *Frame
	nilFrame.CopyFrom(large)
}

func TestCopyFromDoesNotAllocateOnceSized(t *testing.T) {
	src := copyTestFrame(t, 40, 10, 'a')
	dst := src.Clone()
	dst.CopyFrom(src)
	if n := testing.AllocsPerRun(20, func() { dst.CopyFrom(src) }); n != 0 {
		t.Fatalf("CopyFrom allocated %v times", n)
	}
}

func TestRowsEqual(t *testing.T) {
	a := copyTestFrame(t, 10, 4, 'a')
	b := a.Clone()
	for y := range a.Height {
		if !RowsEqualAt(a, y, b, y) {
			t.Fatalf("clone row %d differs", y)
		}
	}

	// Equal content with different physical rotation and different ID tables.
	c := NewFrame(a.Width, a.Height)
	// Intern an unrelated style first so IDs differ from a's.
	c.Set(0, 0, Cell{Rune: 'x', Style: Style{Inverse: true}})
	for y := range a.Height {
		for x := range a.Width {
			c.Set(x, y, a.Cell(x, y))
		}
	}
	for y := range a.Height {
		if !RowsEqualAt(a, y, c, y) {
			t.Fatalf("semantically equal row %d reported different", y)
		}
		if got, want := RowsEqualAt(a, y, c, y), reflect.DeepEqual(a.Row(y), c.Row(y)); got != want {
			t.Fatalf("row %d: RowsEqualAt=%v DeepEqual=%v", y, got, want)
		}
	}

	// Each kind of difference is detected on the right row only.
	diffs := []Cell{
		{Rune: 'Q', Style: a.Cell(3, 1).Style},
		{Rune: a.Cell(3, 1).Rune, Style: Style{Bold: true, Italic: true, Foreground: 5, Background: -1}},
		{Rune: a.Cell(3, 1).Rune, Style: a.Cell(3, 1).Style, Payload: copyTestPayload(t, "z", "")},
		{Rune: a.Cell(3, 1).Rune, Style: a.Cell(3, 1).Style, Continuation: true},
	}
	for i, d := range diffs {
		m := a.Clone()
		m.Set(3, 1, d)
		for y := range a.Height {
			want := y != 1
			if got := RowsEqualAt(a, y, m, y); got != want {
				t.Fatalf("diff %d row %d: got %v want %v", i, y, got, want)
			}
		}
	}

	// Payload present on one side only, and differing payloads.
	m := a.Clone()
	px := -1
	for x := range a.Width {
		if a.Cell(x, 0).Payload != (CellPayload{}) {
			px = x
			break
		}
	}
	if px < 0 {
		t.Fatal("test frame row 0 has no payload")
	}
	m.Set(px, 0, Cell{Rune: a.Cell(px, 0).Rune, Style: a.Cell(px, 0).Style})
	if RowsEqualAt(a, 0, m, 0) {
		t.Fatal("payload removal not detected")
	}

	// Inactive RGB fields and other equivalent spellings compare equal.
	e1, e2 := NewFrame(2, 1), NewFrame(2, 1)
	s1 := Style{Bold: true, Foreground: 2, Background: -1}
	s2 := s1
	s2.ForegroundRGB = RGB{R: 9}
	e1.Set(0, 0, Cell{Rune: 'a', Style: s1})
	e2.Set(0, 0, Cell{Rune: 'a', Style: s2})
	if !RowsEqualAt(e1, 0, e2, 0) {
		t.Fatal("equivalent styles reported different")
	}

	// Mismatched shape or range is unequal and never panics.
	if RowsEqualAt(a, 0, NewFrame(a.Width+1, a.Height), 0) || RowsEqualAt(a, -1, b, -1) || RowsEqualAt(a, a.Height, b, a.Height) || RowsEqualAt(Frame{}, 0, Frame{}, 0) {
		t.Fatal("invalid comparison reported equal")
	}
}

func TestRowsEqualDoesNotAllocate(t *testing.T) {
	a := copyTestFrame(t, 80, 4, 'a')
	b := a.Clone()
	if n := testing.AllocsPerRun(20, func() { RowsEqualAt(a, 2, b, 2) }); n != 0 {
		t.Fatalf("RowsEqualAt allocated %v times", n)
	}
}

func TestCopyFromHandlesEveryPageField(t *testing.T) {
	typ := reflect.TypeOf(cellPage{})
	if typ.NumField() != cellPageFieldCount {
		t.Fatalf("cellPage has %d fields, CopyFrom handles %d: update CopyFrom and cellPageFieldCount", typ.NumField(), cellPageFieldCount)
	}

	// Build a source that populates every field, and a destination whose every
	// field holds different, stale state.
	src := copyTestFrame(t, 9, 5, 'a')
	src.Set(0, 0, Cell{Rune: 'a', Style: Style{Bold: true, Foreground: 7, Background: -1}})
	dst := copyTestFrame(t, 9, 5, 'k')
	dst.Set(4, 4, Cell{Rune: 'f', Style: Style{Italic: true, Foreground: 1, Background: -1}})
	dst.Set(4, 4, Cell{Rune: 'f', Style: DefaultStyle()}) // frees a style slot
	dst.CopyFrom(src)

	sp, dp := reflect.ValueOf(*src.page), reflect.ValueOf(*dst.page)
	populated := 0
	for i := range typ.NumField() {
		name := typ.Field(i).Name
		a, b := sp.Field(i), dp.Field(i)
		if !a.IsZero() {
			populated++
		}
		if (a.Kind() == reflect.Slice || a.Kind() == reflect.Map) && a.Len() == 0 && b.Len() == 0 {
			continue // nil and empty are equivalent
		}
		// Printing works on unexported fields and treats nil and empty alike;
		// fmt sorts map keys, so the rendering is deterministic.
		if got, want := fmt.Sprintf("%#v", b), fmt.Sprintf("%#v", a); got != want {
			t.Errorf("cellPage.%s = %s, want %s", name, got, want)
		}
	}
	if populated < typ.NumField()-2 { // freeStyles/freePayloads may legitimately be empty
		t.Errorf("source populates only %d of %d page fields; strengthen the test", populated, typ.NumField())
	}
}

func TestCopyFromDropsStaleStyleCache(t *testing.T) {
	styleA := Style{Bold: true, Foreground: 1, Background: -1}
	styleB := Style{Italic: true, Foreground: 2, Background: -1}

	// dst: style A lives in slot k and is cached by the last Set.
	dst := NewFrame(4, 2)
	dst.Set(0, 0, Cell{Rune: 'a', Style: styleA})
	k := dst.page.styleCacheID
	if !dst.page.styleCacheOK || dst.page.styles[k].style != styleA {
		t.Fatalf("setup: cache = (%v,%d)", dst.page.styleCacheOK, k)
	}

	// src: a different style occupies the same slot k, and no cache.
	src := NewFrame(4, 2)
	src.Set(0, 0, Cell{Rune: 'b', Style: styleB})
	src.page.styleCacheOK = false
	if src.page.styleIndex[styleB] != k {
		t.Fatalf("setup: style B in slot %d, want %d", src.page.styleIndex[styleB], k)
	}

	dst.CopyFrom(src)
	if dst.page.styleCacheOK && dst.page.styles[dst.page.styleCacheID].style != dst.page.styleCache {
		t.Fatal("CopyFrom left a stale style cache")
	}
	cell := Cell{Rune: 'c', Style: styleA}
	dst.Set(1, 1, cell)
	if got := dst.Cell(1, 1); !got.Equal(cell) {
		t.Fatalf("cell = %+v, want %+v", got, cell)
	}
	if got := dst.Cell(0, 0); got.Style != styleB {
		t.Fatalf("copied cell style = %+v, want %+v", got.Style, styleB)
	}
	if err := dst.CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestRowsEqualAt(t *testing.T) {
	a := copyTestFrame(t, 10, 5, 'a')
	b := a.Clone()
	// b row y+1 shifted: compare a row y+1 against b row y after scrolling b.
	b.ScrollUp(0, 4, 1)
	for y := range 4 {
		if !RowsEqualAt(a, y+1, b, y) {
			t.Fatalf("row %d of a != row %d of scrolled b", y+1, y)
		}
	}
	if RowsEqualAt(a, 0, b, 0) || RowsEqualAt(a, 0, b, 5) || RowsEqualAt(a, -1, b, 0) {
		t.Fatal("unequal or out-of-range rows reported equal")
	}
}

func TestRowsEqualAtRejectsCorruptPayloadIDs(t *testing.T) {
	a := copyTestFrame(t, 4, 1, 'a')
	b := a.Clone()
	b.page.cells[b.page.rows[0]].payloadID = uint32(len(b.page.payloads) + 5)
	if RowsEqualAt(a, 0, b, 0) || RowsEqualAt(b, 0, a, 0) {
		t.Fatal("corrupt payload ID reported equal")
	}
}
