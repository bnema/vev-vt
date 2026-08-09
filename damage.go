package vt

import "github.com/bnema/vev-vt/core"

type Damage = vtcore.Damage

type DamageKind = vtcore.DamageKind

const (
	DamageText       = vtcore.DamageText
	DamageClear      = vtcore.DamageClear
	DamageScrollUp   = vtcore.DamageScrollUp
	DamageFullRedraw = vtcore.DamageFullRedraw
)
