package ansi

import "github.com/bnema/vev-vt/core"

type DamageKind = core.DamageKind
type Damage = core.Damage

const (
	DamageText       = core.DamageText
	DamageClear      = core.DamageClear
	DamageScrollUp   = core.DamageScrollUp
	DamageFullRedraw = core.DamageFullRedraw
)

var FullRedraw = core.FullRedraw
