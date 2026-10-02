package core

import (
	"reflect"
	"testing"
)

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
		if !RowsEqual(a, b, y) {
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
		if !RowsEqual(a, c, y) {
			t.Fatalf("semantically equal row %d reported different", y)
		}
		if got, want := RowsEqual(a, c, y), reflect.DeepEqual(a.Row(y), c.Row(y)); got != want {
			t.Fatalf("row %d: RowsEqual=%v DeepEqual=%v", y, got, want)
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
			if got := RowsEqual(a, m, y); got != want {
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
	if RowsEqual(a, m, 0) {
		t.Fatal("payload removal not detected")
	}

	// Inactive RGB fields and other equivalent spellings compare equal.
	e1, e2 := NewFrame(2, 1), NewFrame(2, 1)
	s1 := Style{Bold: true, Foreground: 2, Background: -1}
	s2 := s1
	s2.ForegroundRGB = RGB{R: 9}
	e1.Set(0, 0, Cell{Rune: 'a', Style: s1})
	e2.Set(0, 0, Cell{Rune: 'a', Style: s2})
	if !RowsEqual(e1, e2, 0) {
		t.Fatal("equivalent styles reported different")
	}

	// Mismatched shape or range is unequal and never panics.
	if RowsEqual(a, NewFrame(a.Width+1, a.Height), 0) || RowsEqual(a, b, -1) || RowsEqual(a, b, a.Height) || RowsEqual(Frame{}, Frame{}, 0) {
		t.Fatal("invalid comparison reported equal")
	}
}

func TestRowsEqualDoesNotAllocate(t *testing.T) {
	a := copyTestFrame(t, 80, 4, 'a')
	b := a.Clone()
	if n := testing.AllocsPerRun(20, func() { RowsEqual(a, b, 2) }); n != 0 {
		t.Fatalf("RowsEqual allocated %v times", n)
	}
}
