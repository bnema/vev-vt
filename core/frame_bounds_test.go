package core

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFrameColumnsCannotAddressAdjacentRows(t *testing.T) {
	for _, x := range []int{-1, 3, 4, math.MaxInt} {
		f := NewFrame(3, 3)
		f.Set(0, 1, Cell{Rune: 'x', Style: DefaultStyle()})
		f.ScrollUp(0, 2, 1)
		before := f.Clone()
		for y := range f.Height {
			require.Panics(t, func() { f.Cell(x, y) })
			require.Panics(t, func() { f.Set(x, y, Cell{Rune: 'z'}) })
		}
		for y := range f.Height {
			require.Equal(t, before.Row(y), f.Row(y))
		}
		require.NoError(t, f.CheckInvariants())
	}
}

func TestFillRowClipsColumns(t *testing.T) {
	f := NewFrame(3, 3)
	f.ScrollDown(0, 2, 1)
	f.FillRow(1, math.MinInt, math.MaxInt, Cell{Rune: 'x', Style: DefaultStyle()})
	for y := range f.Height {
		for x := range f.Width {
			want := rune(' ')
			if y == 1 {
				want = 'x'
			}
			require.Equal(t, want, f.Cell(x, y).Rune)
		}
	}
	require.NoError(t, f.CheckInvariants())
}

func TestReplaceInvalidSourcePreservesDestination(t *testing.T) {
	for _, src := range []Frame{{}, {Width: 2, Height: 2}, NewFrame(0, 2), NewFrame(2, 0)} {
		dst := NewFrame(2, 2)
		dst.Set(1, 1, Cell{Rune: 'x', Style: DefaultStyle()})
		before := dst.Clone()
		dst.Replace(src)
		require.Equal(t, before.Width, dst.Width)
		require.Equal(t, before.Height, dst.Height)
		for y := range dst.Height {
			require.Equal(t, before.Row(y), dst.Row(y))
		}
		require.NoError(t, dst.CheckInvariants())
	}
}
