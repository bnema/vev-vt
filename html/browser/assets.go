package browser

import _ "embed"

//go:embed terminal.js
var javascript string

// JavaScript returns the static dependency-free browser runtime.
func JavaScript() string { return javascript }
