// Package vtcore defines the frontend-neutral terminal model shared by the VT
// emulator and concrete ANSI renderers.
//
// Frame and other mutable values are single-owner. CellSource provides
// storage-independent semantic reads, Frame.Row returns owned storage, and
// Frame.Clone returns an independent frame. The package has no frontend,
// transport, or vev policy.
package core
