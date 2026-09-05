package core

import "testing"

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
					f.Set(x, 0, cell)
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
