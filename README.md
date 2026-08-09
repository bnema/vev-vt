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

## Checks

```sh
go test ./...
go test ./... -race
go vet ./...
go test ./... -run '^$' -bench='.' -benchmem
```
