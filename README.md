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

## Compatibility

VTH3 history bytes are canonical and are decoded strictly, including malformed,
truncated, and trailing input rejection. VEVS is an application-owned outer
snapshot envelope and is intentionally not implemented here.

The module has no production dependencies. `github.com/stretchr/testify` is
used only by the test suite. Keep the public v0.x API and byte formats immutable
once released; behavior changes require explicit versioning and compatibility
evidence.

## Kitty graphics subset

The VT accepts bounded static direct transmissions used by current
`kitten icat --transfer-mode=stream`: PNG, RGB, and RGBA assets; transmit,
transmit-and-display, place, query, and supported delete operations; chunked
Base64 uploads; source rectangles; cell extents; pixel offsets; z-index; and
cursor movement policy. Assets and decoded pixels are bounded by explicit scene
and parser limits. File, temporary-file, shared-memory, animation, composition,
relative placement, and Unicode-placeholder commands remain unsupported.

`Screen` allocates graphics state only after a Kitty APC. Graphics snapshots are
separate from cells and history bytes; VTH3 remains text/history-only.

## Checks

```sh
go test ./...
go test ./... -race
go vet ./...
go test ./... -run '^$' -bench='.' -benchmem
```
