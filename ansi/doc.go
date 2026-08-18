// Package ansi emits ANSI terminal output from the frontend-neutral
// github.com/bnema/vev-vt/core model.
//
// The package owns output planning, ANSI encoding, and a transactional renderer
// shadow. It does not own Cell, Style, RGB, Frame, Damage, or RuneWidth; those
// values are aliases of the core package and are owned by core.
//
// [New] preserves the historical truecolor behavior. A renderer created by
// [NewWithColorProfile] projects every color-bearing style field to its chosen
// [ColorProfile]. [ColorProfileTrueColor] preserves RGB and indexed colors
// exactly as earlier releases did. [ColorProfileANSI256] converts RGB colors to
// the nearest fixed xterm color in indices 16 through 255 while preserving
// valid indexed colors. [ColorProfileANSI16] preserves indices
// 0 through 15, converts other foreground and background colors to the nearest
// canonical ANSI color, and omits explicit underline colors.
// [ColorProfileMonochrome] omits foreground, background, and underline colors
// while preserving non-color attributes.
//
// RGB conversion minimizes squared distance in sRGB space and chooses the lower
// palette index on ties. ANSI-256 conversion compares the 6x6x6 color cube with
// the 24-step gray ramp; it does not generate theme-dependent indices 0 through
// 15. ANSI-16 output uses basic 30-37, 40-47, 90-97, and 100-107 SGR codes.
//
// Constrained profiles omit indexed values outside 0 through 255. Color
// projection is identical for full and incremental draws. Renderer
// configuration is fixed for its lifetime; construct a new Renderer when the
// output target changes.
package ansi
