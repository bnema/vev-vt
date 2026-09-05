# Safe API use

[Back to the README](../README.md)

## Screens and snapshots

A `Screen` changes as bytes arrive. Keep writes, resizes, reads and snapshot
capture in one goroutine, or use a lock in your application. The library does
not provide a screen-wide lock.

`screen.Snapshot()` makes an independent screen copy. You can keep that snapshot
while the live screen changes. History views similarly preserve the rows they
captured. Old, finished history chunks are shared instead of copied repeatedly.

`Screen`, `ScreenSnapshot` and `core.Frame` all support `core.CellSource`: read
dimensions with `Columns()` and `Rows()`, and a cell with `Cell(x, y)`. Renderers
should use these methods rather than assuming a particular memory layout.

Rows returned by public extraction methods are copies. For a writable
`core.Frame`, use `Set`, `WriteRow`, `FillRow`, `CopyRow` and the scroll operations.
A plain Go assignment of a `Frame` shares its storage; use `Clone()` when you need
an independent grid.

## Callbacks

Callbacks run synchronously, before `Screen.Write` returns. Avoid slow work in
them. Copy response bytes during the callback if you need to retain them.

## Styles

Use `DefaultStyle()` to leave foreground and background colors at the terminal's
defaults. `Style{}` is different: its zero color indexes are active colors.

For style comparison, use `Style.Equal`. `Style.Canonical()` removes fields that
do not affect the displayed style, such as an indexed foreground when an RGB
foreground is active. Canonical styles can also be compared with Go's `==`.
This lets storage share identical styles without keeping irrelevant differences.

## Exceptional cell data

`core.NewCellPayload(grapheme, hyperlink)` creates a read-only value for
`Cell.Payload`. It checks UTF-8, rejects terminal control characters and limits
strings to 128 grapheme bytes and 4,096 hyperlink bytes.

Pages share equal values locally and reclaim entries after overwrite or clear.
Copies, resize/reflow, snapshots, history and persistence preserve those values.
Callers never receive internal page handles.

**This is storage support only.** It does not add full grapheme segmentation or
an OSC 8 hyperlink parser. The VT parser and ANSI renderer still use their
existing rune-based text behavior.

## Stability and dependencies

vev-vt is v0.x software. APIs and saved-history formats may change without
compatibility adapters. Check the history guide before updating an application
that persists terminal state.

There are no third-party production dependencies. `testify` is used by tests.
