package ansi

import "github.com/bnema/vev-vt/core"

type DamageKind = core.DamageKind
type Damage = core.Damage

const (
	DamageText       = core.DamageText
	DamageClear      = core.DamageClear
	DamageScrollUp   = core.DamageScrollUp
	DamageScrollDown = core.DamageScrollDown
	DamageFullRedraw = core.DamageFullRedraw
)

var FullRedraw = core.FullRedraw
