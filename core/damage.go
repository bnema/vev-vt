package core

type DamageKind int

const (
	DamageText DamageKind = iota
	DamageClear
	DamageScrollUp
	DamageFullRedraw
	DamageScrollDown
)

type Damage struct {
	Kind   DamageKind
	X      int
	Y      int
	Width  int
	Height int
	Count  int
}

func FullRedraw() Damage { return Damage{Kind: DamageFullRedraw, Count: 1} }
