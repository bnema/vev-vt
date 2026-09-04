package vt

import "github.com/bnema/vev-vt/core"

// The root package re-exports the frontend-neutral core model so screen,
// history, and renderer consumers share one cell/style/frame representation.
// The implementation and storage policy remain owned by the core package.
type Cell = core.Cell
type RGB = core.RGB
type StyleAttrs = core.StyleAttrs
type UnderlineStyle = core.UnderlineStyle
type Style = core.Style
type Frame = core.Frame
type CellSource = core.CellSource

type DamageKind = core.DamageKind
type Damage = core.Damage

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

	DamageText       = core.DamageText
	DamageClear      = core.DamageClear
	DamageScrollUp   = core.DamageScrollUp
	DamageFullRedraw = core.DamageFullRedraw
)

var (
	BlankCell    = core.BlankCell
	DefaultStyle = core.DefaultStyle
	FullRedraw   = core.FullRedraw
	NewFrame     = core.NewFrame
)

func RuneWidth(r rune) int { return core.RuneWidth(r) }
