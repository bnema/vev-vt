package ansi

import "github.com/bnema/vev-vt/core"

type RGB = core.RGB
type StyleAttrs = core.StyleAttrs
type UnderlineStyle = core.UnderlineStyle
type Style = core.Style

const (
	AttrDim           = core.AttrDim
	AttrUnderline     = core.AttrUnderline
	AttrBlink         = core.AttrBlink
	AttrStrikethrough = core.AttrStrikethrough

	UnderlineNone   = core.UnderlineNone
	UnderlineSingle = core.UnderlineSingle
	UnderlineDouble = core.UnderlineDouble
	UnderlineCurly  = core.UnderlineCurly
	UnderlineDotted = core.UnderlineDotted
	UnderlineDashed = core.UnderlineDashed
)

var DefaultStyle = core.DefaultStyle
