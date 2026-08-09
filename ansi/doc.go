// Package ansi emits ANSI terminal output from the frontend-neutral
// github.com/bnema/vev-vt/core model.
//
// The package owns output planning, ANSI encoding, and a transactional renderer
// shadow. It does not own Cell, Style, RGB, Frame, Damage, or RuneWidth; those
// values are aliases of the core package and are owned by core.
package ansi
