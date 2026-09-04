# vev-vt

`github.com/bnema/vev-vt` is a frontend-neutral VT terminal engine for Go.
It provides the terminal screen, scrollback history, immutable snapshots,
stable history chunks, VTH3 history bytes, and the reusable cell/frame model.
The `ansi` package turns core frames and damage into transactional ANSI output.

## Packages

- The module root (`github.com/bnema/vev-vt`) owns VT parsing, screen state,
  history, snapshots, callbacks, and the public model aliases (`Cell`, `Style`,
  `RGB`, `Frame`, `Damage`, and `RuneWidth`).
- `github.com/bnema/vev-vt/core` owns the frontend-neutral cell, style, frame,
  damage, and width implementation. It has no terminal, renderer, transport,
  or application dependencies.
- `github.com/bnema/vev-vt/ansi` is the concrete ANSI output package. It consumes
  core frames and damage; it does not define a renderer-backend interface.
- `github.com/bnema/vev-vt/graphics` owns bounded renderer-neutral raster assets,
  sparse placements, clipping fragments, and immutable scene snapshots. It has no
  Kitty, ANSI, VT-policy, or transport dependency.
- `github.com/bnema/vev-vt/protocol/kittygraphics` parses bounded Kitty graphics
  APC commands and translates the supported static/direct subset into `graphics`.

## Ownership contract

Stateful values are single-owner and are not internally synchronized. Serialize
screen writes, resizes, snapshots, and history mutations in one owner. Parsing
and callbacks are synchronous: callbacks run before `Write` returns and follow
parser event order. Consume or copy response bytes during the callback.

`Snapshot`, `HistorySnapshotView`, and `Row` methods provide owned captures or
copies. `BorrowedRow`, `Frame.Row`, and `HistoryView.Range` expose borrowed
storage with the lifetimes documented by their APIs; borrowed storage must not
be mutated or retained beyond its documented lifetime. Sealed `HistoryChunk`
identity is stable for the lifetime of a view.

## Alpha stability

`DefaultStyle()` is the canonical unset style and differs from `Style{}`, whose
zero-valued color indexes are active. `Style.Canonical()` clears inactive color
payloads and produces a representation whose Go equality exactly matches
`Style.Equal`; it does not collapse `Style{}` into `DefaultStyle()`.

The module has no production dependencies. `github.com/stretchr/testify` is
used only by the test suite. During v0.x alpha development, public APIs and byte
formats may change without compatibility adapters or migration readers. Prefer
a direct, documented break and removal of obsolete code when that produces a
cleaner design. VEVS remains an application-owned outer snapshot envelope and
is intentionally not implemented here.

## Kitty graphics subset

The VT accepts bounded static direct transmissions used by current
`kitten icat --transfer-mode=stream`: PNG, RGB, and RGBA assets; transmit,
transmit-and-display, place, query, and supported delete operations; chunked
Base64 uploads with optional zlib compression; source rectangles; cell extents;
pixel offsets; z-index; and cursor movement policy. Assets and decoded pixels
are bounded by explicit scene and parser limits. File, temporary-file,
shared-memory, animation, composition, relative placement, and
Unicode-placeholder commands remain unsupported.

`Screen` allocates graphics state only after a Kitty APC. Graphics snapshots are
separate from cells and history bytes; VTH3 remains text/history-only. Static
placements move with terminal row scrolling and are clipped by the active
viewport; reflow and relative-placement movement remain unsupported.

### Headless graphics harness

`cmd/graphics-harness` feeds a terminal byte capture into a fresh `Screen` and
writes its active graphics snapshot as a PNG using an internal reference
compositor. It is a development inspection tool, not a terminal emulator.

```sh
go run ./cmd/graphics-harness \
  -input internal/graphicsharness/testdata/demo.apc \
  -output /tmp/graphics-harness.png \
  -cols 4 -rows 4 -pixel-width 4 -pixel-height 4 -scale 64
```

The included demo has an opaque blue background and a semi-transparent red
overlay. Its output is suitable for direct image inspection.

## Checks

```sh
go test ./...
go test ./... -race
go vet ./...
go test ./... -run '^$' -bench='.' -benchmem
```

The storage redesign baseline from issue #9 can be measured independently with:

```sh
go test . -run '^$' \
  -bench '^BenchmarkHistory(Build|Retained)10Kx120$' \
  -benchmem -benchtime=1x -count=1
```

It reports construction allocations and retained heap bytes for plain ASCII,
repeated indexed and RGB styles, high-cardinality RGB churn, wide Unicode, and
styled blanks.
