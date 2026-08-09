package vt

import "github.com/bnema/vev-vt/core"

// The root package re-exports the frontend-neutral core model so screen,
// history, and renderer consumers share one cell/style/frame representation.
// The implementation and storage policy remain owned by the core package.
type Cell = vtcore.Cell
type RGB = vtcore.RGB
type StyleAttrs = vtcore.StyleAttrs
type UnderlineStyle = vtcore.UnderlineStyle
type Style = vtcore.Style
type Frame = vtcore.Frame

const (
	AttrDim           = vtcore.AttrDim
	AttrUnderline     = vtcore.AttrUnderline
	AttrBlink         = vtcore.AttrBlink
	AttrStrikethrough = vtcore.AttrStrikethrough

	UnderlineNone   = vtcore.UnderlineNone
	UnderlineSingle = vtcore.UnderlineSingle
	UnderlineDouble = vtcore.UnderlineDouble
	UnderlineCurly  = vtcore.UnderlineCurly
	UnderlineDotted = vtcore.UnderlineDotted
	UnderlineDashed = vtcore.UnderlineDashed
)

var (
	BlankCell    = vtcore.BlankCell
	DefaultStyle = vtcore.DefaultStyle
	FullRedraw   = vtcore.FullRedraw
	NewFrame     = vtcore.NewFrame
)

func RuneWidth(r rune) int { return vtcore.RuneWidth(r) }
