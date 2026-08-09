package ansi

import "github.com/bnema/vev-vt/core"

// RuneWidth is the terminal cell width function from the frontend-neutral
// model package.
func RuneWidth(r rune) int { return vtcore.RuneWidth(r) }
