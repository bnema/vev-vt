package html

import (
	"testing"

	"github.com/bnema/vev-vt/core"
	"github.com/stretchr/testify/require"
)

func TestRendererCanonicalizesEveryStyleField(t *testing.T) {
	style := core.DefaultStyle()
	style.Bold = true
	style.Italic = true
	style.Inverse = true
	style.Attrs = core.AttrDim | core.AttrUnderline | core.AttrBlink | core.AttrStrikethrough
	style.Foreground = 12
	style.HasBackgroundRGB = true
	style.BackgroundRGB = core.RGB{R: 1, G: 2, B: 3}
	style.UnderlineStyle = core.UnderlineCurly
	style.HasUnderlineColor = true
	style.UnderlineColor = 200

	frame := core.NewFrame(1, 1)
	frame.Set(0, 0, core.Cell{Rune: 'X', Style: style})
	renderer, err := New(Options{})
	require.NoError(t, err)
	prepared, err := renderer.Prepare(frame, nil, false, Cursor{})
	require.NoError(t, err)
	require.Equal(t, []Style{{
		Bold: true, Italic: true, Inverse: true, Dim: true, Blink: true,
		Strikethrough: true, Underline: true, UnderlineStyle: core.UnderlineCurly,
		Foreground:     Color{Kind: ColorIndexed, Index: 12},
		Background:     Color{Kind: ColorRGB, RGB: RGB{R: 1, G: 2, B: 3}},
		UnderlineColor: Color{Kind: ColorIndexed, Index: 200},
	}}, prepared.Update().Styles)
}
