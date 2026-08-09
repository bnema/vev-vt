package ansi

import "github.com/bnema/vev-vt/core"

// Cell is an alias for the frontend-neutral core model. New code should import
// github.com/bnema/vev-vt/core when it needs the model directly.
type Cell = vtcore.Cell

var BlankCell = vtcore.BlankCell
