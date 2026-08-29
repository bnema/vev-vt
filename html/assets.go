package html

import _ "embed"

//go:embed terminal.css
var stylesheet string

// Stylesheet returns the static structural terminal CSS.
func Stylesheet() string { return stylesheet }
