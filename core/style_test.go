package core

import (
	"testing"
	"testing/quick"
)

func TestStyleCanonicalClearsInactiveColorPayloads(t *testing.T) {
	style := Style{
		Foreground:           7,
		Background:           8,
		HasForegroundRGB:     true,
		ForegroundRGB:        RGB{R: 1, G: 2, B: 3},
		BackgroundRGB:        RGB{R: 4, G: 5, B: 6},
		UnderlineColor:       9,
		UnderlineColorRGB:    RGB{R: 7, G: 8, B: 9},
		HasUnderlineColorRGB: true,
	}

	got := style.Canonical()
	if got.Foreground != 0 {
		t.Fatalf("inactive indexed foreground = %d, want 0", got.Foreground)
	}
	if got.BackgroundRGB != (RGB{}) {
		t.Fatalf("inactive RGB background = %+v, want zero", got.BackgroundRGB)
	}
	if got.UnderlineColor != 0 || got.HasUnderlineColor {
		t.Fatalf("inactive indexed underline = (%d, %t), want zero and false", got.UnderlineColor, got.HasUnderlineColor)
	}
}

func TestStyleCanonicalCollapsesEveryInactiveVariant(t *testing.T) {
	tests := []struct {
		name  string
		left  Style
		right Style
	}{
		{
			name: "indexed colors ignore RGB payloads and absent underline payloads",
			left: Style{Foreground: 1, Background: 2},
			right: Style{
				Foreground:        1,
				ForegroundRGB:     RGB{R: 1, G: 2, B: 3},
				Background:        2,
				BackgroundRGB:     RGB{R: 4, G: 5, B: 6},
				UnderlineColor:    3,
				UnderlineColorRGB: RGB{R: 7, G: 8, B: 9},
			},
		},
		{
			name: "RGB colors ignore indexed payloads",
			left: Style{
				HasForegroundRGB:     true,
				ForegroundRGB:        RGB{R: 1, G: 2, B: 3},
				HasBackgroundRGB:     true,
				BackgroundRGB:        RGB{R: 4, G: 5, B: 6},
				HasUnderlineColorRGB: true,
				UnderlineColorRGB:    RGB{R: 7, G: 8, B: 9},
			},
			right: Style{
				Foreground:           1,
				Background:           2,
				HasForegroundRGB:     true,
				ForegroundRGB:        RGB{R: 1, G: 2, B: 3},
				HasBackgroundRGB:     true,
				BackgroundRGB:        RGB{R: 4, G: 5, B: 6},
				HasUnderlineColor:    true,
				UnderlineColor:       3,
				HasUnderlineColorRGB: true,
				UnderlineColorRGB:    RGB{R: 7, G: 8, B: 9},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !test.left.Equal(test.right) {
				t.Fatal("fixture styles must be semantically equal")
			}
			if test.left.Canonical() != test.right.Canonical() {
				t.Fatalf("canonical styles differ: %+v != %+v", test.left.Canonical(), test.right.Canonical())
			}
		})
	}
}

func TestStyleCanonicalExactlyMatchesEqual(t *testing.T) {
	property := func(a, b Style) bool {
		return a.Equal(b) == (a.Canonical() == b.Canonical())
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 10_000}); err != nil {
		t.Fatal(err)
	}
}

func TestStyleCanonicalIsIdempotent(t *testing.T) {
	property := func(style Style) bool {
		canonical := style.Canonical()
		return canonical.Canonical() == canonical
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 10_000}); err != nil {
		t.Fatal(err)
	}
}

func TestZeroAndDefaultStylesRemainDistinct(t *testing.T) {
	zero := Style{}
	defaultStyle := DefaultStyle()
	if zero.Equal(defaultStyle) {
		t.Fatal("zero Style unexpectedly equals DefaultStyle")
	}
	if zero.Canonical() == defaultStyle.Canonical() {
		t.Fatal("canonicalization collapsed indexed color zero into unset default colors")
	}
}
