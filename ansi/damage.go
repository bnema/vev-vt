package ansi

import "github.com/bnema/vev-vt/core"

type DamageKind = vtcore.DamageKind

type Damage = vtcore.Damage

const (
	DamageText       = vtcore.DamageText
	DamageClear      = vtcore.DamageClear
	DamageScrollUp   = vtcore.DamageScrollUp
	DamageFullRedraw = vtcore.DamageFullRedraw
)

var FullRedraw = vtcore.FullRedraw
