package core

import "testing"

func TestBlankCellIsSpaceWithDefaultStyle(t *testing.T) {
	want := Cell{Rune: ' ', Style: DefaultStyle()}
	if got := BlankCell(); got != want {
		t.Fatalf("BlankCell() = %+v, want %+v", got, want)
	}
	if BlankCell() == (Cell{}) {
		t.Fatal("blank cell unexpectedly equals the zero Cell")
	}
}

func TestCellEqualUsesSemanticStyleEquality(t *testing.T) {
	leftStyle := Style{
		Foreground:        7,
		HasForegroundRGB:  true,
		ForegroundRGB:     RGB{R: 1, G: 2, B: 3},
		Background:        -1,
		BackgroundRGB:     RGB{R: 4, G: 5, B: 6},
		UnderlineColor:    9,
		UnderlineColorRGB: RGB{R: 7, G: 8, B: 9},
	}
	rightStyle := leftStyle
	rightStyle.Foreground = 99
	rightStyle.BackgroundRGB = RGB{R: 10, G: 11, B: 12}
	rightStyle.UnderlineColor = 42

	left := Cell{Rune: 'x', Style: leftStyle}
	right := Cell{Rune: 'x', Style: rightStyle}
	if !left.Equal(right) {
		t.Fatal("cells with equal active style fields must compare equal")
	}
	if left == right {
		t.Fatal("fixture must differ in inactive raw style fields")
	}
}

func TestContinuationCellSemanticsRemainExplicit(t *testing.T) {
	style := DefaultStyle()
	head := Cell{Rune: '界', Style: style}
	tail := Cell{Style: style, Continuation: true}
	if head.Continuation || head.Rune != '界' {
		t.Fatalf("wide head = %+v", head)
	}
	if !tail.Continuation || tail.Rune != 0 {
		t.Fatalf("wide tail = %+v", tail)
	}
	if head.Equal(tail) {
		t.Fatal("wide head and continuation tail unexpectedly compare equal")
	}
}
